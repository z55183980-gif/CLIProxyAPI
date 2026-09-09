# 原生侧边栏版本

此目录包含配套后端 cli-proxy-api.exe 和 static/management.html。

- 账号管理：/management.html#/accounts
- IP 管理：/management.html#/proxies
- 不提供 /quota 或 /account 到新页面的兼容跳转。
- 后端直接提供前端文件，不再注入旧账号/IP 面板。

部署时使用这两个配套产物。配置中的 remote-management.disable-auto-update-panel 应设为 true，以免上游自动更新覆盖自定义前端。
静态目录默认跟随配置文件目录下的 static；也可以使用 MANAGEMENT_STATIC_PATH 指定本目录中的 static。
本次没有替换正在运行的服务或修改现有配置、账号及代理数据。

前端验证：444 项测试通过，lint、TypeScript 与 Vite 构建通过。
后端验证：管理相关测试通过，Windows 可执行文件构建通过。
浏览器验证：虚拟数据下两个菜单、路由、数据展示和侧边栏折叠正常；未执行真实后端写操作。
