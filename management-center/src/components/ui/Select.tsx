import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
} from 'react';
import { createPortal } from 'react-dom';
import { IconCheck, IconChevronDown } from './icons';
import styles from './Select.module.scss';

export interface SelectOption {
  value: string;
  label: string;
}

interface SelectProps {
  value: string;
  options: ReadonlyArray<SelectOption>;
  onChange: (value: string) => void;
  placeholder?: string;
  className?: string;
  disabled?: boolean;
  ariaLabel?: string;
  ariaLabelledBy?: string;
  ariaDescribedBy?: string;
  fullWidth?: boolean;
  size?: 'sm' | 'md';
  id?: string;
  required?: boolean;
}

const VIEWPORT_MARGIN = 8;
const DROPDOWN_OFFSET = 6;
const DROPDOWN_MAX_HEIGHT = 240;
const DROPDOWN_Z_INDEX = 2010;

const clamp = (value: number, min: number, max: number) => Math.min(Math.max(value, min), max);

const resolveDropdownStyle = (element: HTMLElement): CSSProperties => {
  const rect = element.getBoundingClientRect();
  const viewportWidth = window.innerWidth;
  const viewportHeight = window.innerHeight;
  const width = Math.min(rect.width, Math.max(0, viewportWidth - VIEWPORT_MARGIN * 2));
  const left = clamp(
    rect.left,
    VIEWPORT_MARGIN,
    Math.max(VIEWPORT_MARGIN, viewportWidth - width - VIEWPORT_MARGIN)
  );
  const spaceBelow = viewportHeight - rect.bottom - VIEWPORT_MARGIN - DROPDOWN_OFFSET;
  const spaceAbove = rect.top - VIEWPORT_MARGIN - DROPDOWN_OFFSET;
  const direction = spaceBelow >= DROPDOWN_MAX_HEIGHT || spaceBelow >= spaceAbove ? 'down' : 'up';
  const maxHeight = Math.max(
    0,
    Math.min(DROPDOWN_MAX_HEIGHT, direction === 'down' ? spaceBelow : spaceAbove)
  );

  return direction === 'down'
    ? {
        position: 'fixed',
        top: rect.bottom + DROPDOWN_OFFSET,
        left,
        width,
        maxHeight,
        zIndex: DROPDOWN_Z_INDEX,
      }
    : {
        position: 'fixed',
        bottom: viewportHeight - rect.top + DROPDOWN_OFFSET,
        left,
        width,
        maxHeight,
        zIndex: DROPDOWN_Z_INDEX,
      };
};

export function Select({
  value,
  options,
  onChange,
  placeholder,
  className,
  disabled = false,
  ariaLabel,
  ariaLabelledBy,
  ariaDescribedBy,
  fullWidth = true,
  size = 'md',
  id,
  required = false,
}: SelectProps) {
  const generatedId = useId();
  const selectId = id ?? generatedId;
  const listboxId = `${selectId}-listbox`;
  const [open, setOpen] = useState(false);
  const [highlightedIndex, setHighlightedIndex] = useState(-1);
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const searchRef = useRef({ text: '', time: 0 });
  const dropdownRef = useRef<HTMLDivElement | null>(null);
  const rafRef = useRef<number | null>(null);
  const [dropdownStyle, setDropdownStyle] = useState<CSSProperties | null>(null);
  const isOpen = open && !disabled;

  useEffect(() => {
    if (!open || disabled) return;
    const handleClickOutside = (event: PointerEvent) => {
      const target = event.target as Node;
      if (wrapRef.current?.contains(target) || dropdownRef.current?.contains(target)) return;
      setOpen(false);
    };
    document.addEventListener('pointerdown', handleClickOutside, true);
    const handleFocusOutside = (event: FocusEvent) => {
      if (!wrapRef.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener('focusin', handleFocusOutside);
    return () => {
      document.removeEventListener('pointerdown', handleClickOutside, true);
      document.removeEventListener('focusin', handleFocusOutside);
    };
  }, [disabled, open]);

  const updateDropdownStyle = useCallback(() => {
    if (!wrapRef.current) return;
    setDropdownStyle(resolveDropdownStyle(wrapRef.current));
  }, []);

  const scheduleDropdownStyleUpdate = useCallback(() => {
    if (typeof window === 'undefined') return;
    if (rafRef.current !== null) {
      window.cancelAnimationFrame(rafRef.current);
    }
    rafRef.current = window.requestAnimationFrame(() => {
      rafRef.current = null;
      updateDropdownStyle();
    });
  }, [updateDropdownStyle]);

  useLayoutEffect(() => {
    if (!isOpen) {
      if (rafRef.current !== null && typeof window !== 'undefined') {
        window.cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      }
      return;
    }

    updateDropdownStyle();

    const handleViewportChange = () => {
      scheduleDropdownStyleUpdate();
    };

    const resizeObserver =
      typeof ResizeObserver !== 'undefined' && wrapRef.current
        ? new ResizeObserver(() => {
            scheduleDropdownStyleUpdate();
          })
        : null;

    if (resizeObserver && wrapRef.current) {
      resizeObserver.observe(wrapRef.current);
    }

    window.addEventListener('resize', handleViewportChange);
    window.addEventListener('scroll', handleViewportChange, true);

    return () => {
      window.removeEventListener('resize', handleViewportChange);
      window.removeEventListener('scroll', handleViewportChange, true);
      resizeObserver?.disconnect();
      if (rafRef.current !== null) {
        window.cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      }
    };
  }, [isOpen, scheduleDropdownStyleUpdate, updateDropdownStyle]);

  const selectedIndex = useMemo(
    () => options.findIndex((option) => option.value === value),
    [options, value]
  );
  const resolvedHighlightedIndex =
    highlightedIndex >= 0 && highlightedIndex < options.length
      ? highlightedIndex
      : selectedIndex >= 0
        ? selectedIndex
        : options.length > 0
          ? 0
          : -1;
  const selected = selectedIndex >= 0 ? options[selectedIndex] : undefined;
  const displayText = selected?.label ?? placeholder ?? '';
  const isPlaceholder = !selected && placeholder;

  const openDropdown = useCallback(() => {
    setHighlightedIndex(selectedIndex >= 0 ? selectedIndex : options.length ? 0 : -1);
    searchRef.current = { text: '', time: 0 };
    setOpen(true);
  }, [options.length, selectedIndex]);

  const commitSelection = useCallback(
    (nextIndex: number) => {
      const nextOption = options[nextIndex];
      if (!nextOption) return;
      onChange(nextOption.value);
      setOpen(false);
      setHighlightedIndex(nextIndex);
      triggerRef.current?.focus({ preventScroll: true });
    },
    [onChange, options]
  );

  const moveHighlight = useCallback(
    (direction: 1 | -1) => {
      if (options.length === 0) return;
      const nextIndex = (resolvedHighlightedIndex + direction + options.length) % options.length;
      setHighlightedIndex(nextIndex);
    },
    [options.length, resolvedHighlightedIndex]
  );

  const handleKeyDown = useCallback(
    (event: React.KeyboardEvent<HTMLButtonElement>) => {
      if (disabled) return;

      switch (event.key) {
        case 'ArrowDown':
          event.preventDefault();
          if (!isOpen) {
            openDropdown();
            return;
          }
          moveHighlight(1);
          return;
        case 'ArrowUp':
          event.preventDefault();
          if (!isOpen) {
            openDropdown();
            return;
          }
          moveHighlight(-1);
          return;
        case 'Home':
          if (!isOpen || options.length === 0) return;
          event.preventDefault();
          setHighlightedIndex(0);
          return;
        case 'End':
          if (!isOpen || options.length === 0) return;
          event.preventDefault();
          setHighlightedIndex(options.length - 1);
          return;
        case 'Enter':
        case ' ': {
          event.preventDefault();
          if (!isOpen) {
            openDropdown();
            return;
          }
          if (resolvedHighlightedIndex >= 0) {
            commitSelection(resolvedHighlightedIndex);
          }
          return;
        }
        case 'Escape':
          if (!isOpen) return;
          event.preventDefault();
          event.stopPropagation();
          setOpen(false);
          return;
        case 'Tab':
          if (isOpen) setOpen(false);
          return;
        default:
          if (event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey) {
            const now = Date.now();
            const previous = now - searchRef.current.time < 700 ? searchRef.current.text : '';
            const text = `${previous}${event.key}`.toLocaleLowerCase();
            searchRef.current = { text, time: now };
            const query = [...text].every((character) => character === text[0]) ? text[0] : text;
            const start = isOpen ? resolvedHighlightedIndex : selectedIndex;
            for (let offset = 1; offset <= options.length; offset++) {
              const index = (start + offset + options.length) % options.length;
              if (options[index].label.toLocaleLowerCase().startsWith(query)) {
                event.preventDefault();
                if (isOpen) setHighlightedIndex(index);
                else commitSelection(index);
                break;
              }
            }
          }
          return;
      }
    },
    [
      commitSelection,
      disabled,
      isOpen,
      moveHighlight,
      openDropdown,
      options,
      resolvedHighlightedIndex,
      selectedIndex,
    ]
  );

  useEffect(() => {
    if (!isOpen || resolvedHighlightedIndex < 0) return;
    const highlightedOption = document.getElementById(
      `${selectId}-option-${resolvedHighlightedIndex}`
    );
    highlightedOption?.scrollIntoView({ block: 'nearest' });
  }, [isOpen, resolvedHighlightedIndex, selectId]);

  const dropdown =
    isOpen && dropdownStyle ? (
      <div
        ref={dropdownRef}
        className={styles.dropdown}
        id={listboxId}
        role="listbox"
        aria-label={ariaLabel}
        aria-labelledby={ariaLabelledBy ?? (!ariaLabel ? selectId : undefined)}
        data-direction={dropdownStyle.bottom !== undefined ? 'up' : 'down'}
        style={dropdownStyle}
      >
        {options.map((opt, index) => {
          const active = opt.value === value;
          const highlighted = index === resolvedHighlightedIndex;
          return (
            <div
              key={opt.value}
              id={`${selectId}-option-${index}`}
              role="option"
              aria-selected={active}
              className={`${styles.option} ${active ? styles.optionActive : ''} ${highlighted ? styles.optionHighlighted : ''}`.trim()}
              onMouseEnter={() => setHighlightedIndex(index)}
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => commitSelection(index)}
            >
              <span className={styles.optionLabel}>{opt.label}</span>
              <span className={styles.optionCheck} aria-hidden="true">
                {active && <IconCheck size={15} />}
              </span>
            </div>
          );
        })}
      </div>
    ) : null;

  return (
    <>
      <div
        className={`${styles.wrap} ${fullWidth ? styles.wrapFullWidth : ''} ${className ?? ''}`}
        ref={wrapRef}
      >
        <button
          ref={triggerRef}
          id={selectId}
          type="button"
          className={`${styles.trigger} ${size === 'sm' ? styles.triggerSm : ''}`.trim()}
          onClick={disabled ? undefined : () => (isOpen ? setOpen(false) : openDropdown())}
          onKeyDown={handleKeyDown}
          role="combobox"
          aria-autocomplete="none"
          aria-required={required || undefined}
          aria-haspopup="listbox"
          aria-expanded={isOpen}
          aria-controls={isOpen ? listboxId : undefined}
          aria-activedescendant={
            isOpen && resolvedHighlightedIndex >= 0
              ? `${selectId}-option-${resolvedHighlightedIndex}`
              : undefined
          }
          aria-label={ariaLabel}
          aria-labelledby={ariaLabelledBy}
          aria-describedby={ariaDescribedBy}
          disabled={disabled}
        >
          <span className={`${styles.triggerText} ${isPlaceholder ? styles.placeholder : ''}`}>
            {displayText}
          </span>
          <span className={styles.triggerIcon} aria-hidden="true">
            <IconChevronDown size={14} />
          </span>
        </button>
        {required && (
          <input
            className={styles.validationInput}
            value={selected?.value ?? ''}
            required
            disabled={disabled}
            tabIndex={-1}
            aria-hidden="true"
            onChange={() => {}}
            onInvalid={(event) => {
              event.preventDefault();
              triggerRef.current?.focus();
              openDropdown();
            }}
          />
        )}
      </div>
      {dropdown &&
        (typeof document === 'undefined' ? dropdown : createPortal(dropdown, document.body))}
    </>
  );
}
