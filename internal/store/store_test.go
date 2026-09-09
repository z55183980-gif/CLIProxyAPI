package store

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestMain keeps package tests out of the Windows system temporary directory.
// Go 1.26's os.Root (used by go-billy/go-git) may be unable to traverse
// C:\Windows\Temp under restricted ACLs, causing every clone test to fail with
// an "Access is denied" error even though regular file operations succeed.
func TestMain(m *testing.M) {
	var oldTemp, oldTmp string
	var changed bool
	if runtime.GOOS == "windows" {
		current := os.TempDir()
		vol := filepath.VolumeName(current)
		systemTemp := filepath.Join(vol+string(filepath.Separator), "Windows", "Temp")
		if strings.EqualFold(filepath.Clean(current), filepath.Clean(systemTemp)) {
			if cacheDir, err := os.UserCacheDir(); err == nil {
				// Keep the test root inside the user's conventional Temp directory.
				testTemp := filepath.Join(cacheDir, "Temp", "cliproxy-tests")
				if err := os.MkdirAll(testTemp, 0o700); err == nil {
					oldTemp, oldTmp = os.Getenv("TEMP"), os.Getenv("TMP")
					_ = os.Setenv("TEMP", testTemp)
					_ = os.Setenv("TMP", testTemp)
					changed = true
				}
			}
		}
	}
	code := m.Run()
	if changed {
		_ = os.Setenv("TEMP", oldTemp)
		_ = os.Setenv("TMP", oldTmp)
	}
	os.Exit(code)
}
