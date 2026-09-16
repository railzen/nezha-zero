---
outline: deep
---

# 快速开始

本页介绍如何在 Linux 服务器上安装 Nezha Zero 面板，只使用管理员用户名和密码登录。无需 GitHub 等第三方账号，也无需创建 OAuth 应用。

面板也支持 [OAuth 登录](#oauth-login)：通过 GitHub 等第三方平台授权验证管理员身份，可单独使用，也可与密码登录同时配置。需要此方式时，请先准备 OAuth 应用，再在安装过程中选择配置 OAuth。

## 准备工作

- 一台可以访问 GitHub、具有 root 或 sudo 权限的 Linux 服务器。
- 服务器上可以使用 `curl` 命令。
- 放行面板访问端口和 Agent 通信端口，默认分别为 TCP `8008` 和 `5555`。云服务器还需检查安全组。

可以先通过服务器 IP 访问面板，域名和反向代理可在安装完成后配置。

## 选择安装方式

以下两种方式任选一种。它们使用同一个安装脚本，区别在于面板运行在 Docker 容器中，还是直接作为系统服务运行。

### Docker 安装

在服务器终端执行：

```bash
curl -L https://raw.githubusercontent.com/railzen/nezha-zero/main/script/naza.sh -o nezha.sh && chmod +x nezha.sh && sudo ./nezha.sh install_dashboard
```

在安装方式提示中选择 `1. Docker`，输入 `1` 并回车。首次安装时，如果未安装 Docker，脚本会尝试安装。已有 Docker 的服务器需要有可用的 `docker compose` 或 `docker-compose`；若脚本提示缺少 Compose，请先补齐后重新运行。

随后按下方[配置密码登录](#password-login)或[配置 OAuth 登录](#oauth-login)完成设置，脚本会拉取面板镜像并启动容器。

### 直接安装（独立安装）

在服务器终端执行：

```bash
curl -L https://raw.githubusercontent.com/railzen/nezha-zero/main/script/naza.sh -o nezha.sh && chmod +x nezha.sh && sudo ./nezha.sh install_dashboard
```

在安装方式提示中选择 `2. 独立安装`，输入 `2` 并回车。此方式不需要 Docker，脚本会下载面板程序，并配置系统服务（通常为 systemd，Alpine 使用 OpenRC）。

随后按下方步骤[配置密码登录](#password-login)或[配置 OAuth 登录](#oauth-login)。两种方式的安装数据均位于 `/opt/nezha/dashboard`。如果服务器已有面板，脚本可能会识别并沿用已有安装方式，不再显示安装方式选择。

## 配置密码登录 {#password-login}

1. 在 `是否配置 OAuth 登录？[Y/n]:` 提示处输入 `n` 并回车。不要直接回车，默认选项会启用 OAuth 配置。
2. 输入管理员用户名，例如 `admin`，不能为空，无需与第三方平台账号对应。
3. 输入面板密码；留空回车会生成一个 16 位随机密码。选择不配置 OAuth 后，脚本会自动配置密码登录。
4. 输入站点标题，例如 `我的监控`，不能为空。
5. 输入面板访问端口，直接回车使用默认 `8008`。
6. 输入 Agent 接入的 RPC 端口，直接回车使用默认 `5555`。

配置保存后，脚本会显示管理员用户名、密码及端口，请保存这些信息。`OAuth 登录: 未配置` 是纯密码安装的正常结果。等待脚本完成安装并启动面板。

## 配置 OAuth 登录（可选） {#oauth-login}

OAuth 登录会跳转到第三方平台完成授权，再返回面板。它需要对应平台的账号和 OAuth 应用；只使用密码登录时可跳过本节。

1. 安装前，按[获取 GitHub Client ID 和密钥](/guide/dashboard.html#获取-github-的-client-id-和密钥)创建应用，保存 Client ID 和 Client Secret，并填写面板的回调地址。也可以参考 [Cloudflare Access 配置](/guide/q8.html)或 [OIDC 配置](/guide/q10.html)。
2. 在 `是否配置 OAuth 登录？[Y/n]:` 提示处输入 `y` 或直接回车，按提示填写提供商、Client ID 和 Client Secret。
3. 输入 OAuth 平台中的管理员用户名/User ID，多个以逗号分隔；这里应填写平台对应的身份，而不是随意起一个本地用户名。
4. 在 `是否配置密码登录？[Y/n]:` 提示处，输入 `n` 表示仅使用 OAuth；输入 `y` 或直接回车则同时配置密码登录，并继续设置面板密码。
5. 填写站点标题、面板端口和 Agent RPC 端口，等待安装完成。更详细的说明见[安装 Dashboard](/guide/dashboard.html)。

## 打开面板

在浏览器中访问 `http://服务器IP:8008`；如果更改了面板端口，请使用实际端口。在面板登录页面输入刚才配置的管理员用户名和密码即可登录。

如果配置了 OAuth，则可点击登录页面的 OAuth 登录入口，完成第三方授权后返回面板。仅配置 OAuth 时，使用该入口登录。

需要使用域名和 HTTPS 时，请继续阅读[配置反向代理](/guide/dashboard.html#配置反向代理)和[配置 SSL](/guide/dashboard.html#在宝塔面板中配置-ssl)。纯密码登录不需要配置 OAuth 回调地址。

## 接入第一台服务器

1. 在管理面板的“设置”中填写 Agent 可直连的面板域名或 IP，使用域名时不要接入 CDN。
2. 在“服务器”页面添加一台服务器，复制对应系统的一键安装命令。
3. 在被监控服务器上执行命令，等待服务器在面板中上线。

详细操作见[安装 Agent](/guide/agent.html)。以后可以在保存脚本的目录运行 `sudo ./nezha.sh`，打开管理菜单进行更新或查看日志。
