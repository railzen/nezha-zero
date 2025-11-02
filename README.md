<div align="center">
  <br>
  <img width="360" style="max-width:80%" src="resource/static/brand.svg" title="哪吒监控 Nezha Monitoring">
  </br>
  <br>
	<a href="https://nezha-v0.mereith.dev/guide/dashboard.html" target="_blank"><img src="https://img.shields.io/badge/Docs-Available-orange?style=for-the-badge&logo=gitbook&logoColor=white" alt="查看文档"></a>
	<a href="https://github.com/railzen/NezhaZero" target="_blank"><img alt="GitHub release (with filter)" src="https://img.shields.io/github/v/release/railzen/NezhaZero?color=brightgreen&style=for-the-badge&logo=github&label=Dashboard"></a>
	<a href="https://github.com/nezhahq/agent/releases/tag/v0.20.5" target="_blank"><img src="https://img.shields.io/badge/Agent-v0.20.5-bridhtgreen?logo=github&style=for-the-badge"></a>
	<a href="https://github.com/nezhahq/nezha" target="_blank"><img src="https://img.shields.io/badge/NEZHA-NAIBA-blue?logo=github&style=for-the-badge" alt="访问哪吒仓库"></a>
  </br>
  <p><b>Nezha Monitoring: Self-hostable, lightweight, servers and websites monitoring tool.</b></p>
  <p>Supports <b>monitoring</b> system status, HTTP, TCP, Ping, <b>push alerts</b> and <b>web terminal</b>.</p>
</div>





## Abstract
基于哪吒V0版本进行二次修改，主要更新了GEOIP库和管理界面安装Agent的链接，同时进行了一些样式优化。

最新Agent版本以上面标签展示为准，放在Release里面仅便于使用。Agent已经关闭自动升级功能，如无必要不会升级。面板将尽可能减少更新以稳定版本。一键安装脚本如下：

```shell
curl -L https://raw.githubusercontent.com/railzen/nezhazero/main/script/naza.sh -o naza.sh && chmod +x naza.sh && ./naza.sh
```



## Screenshots

| Default Theme                                                                                 | DayNight [@JackieSung](https://github.com/JackieSung4ev)                                               | hotaru                                                                     |
| ---------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------- |
| ![Default Theme](resource/template/theme-default/screenshot.png)                              | <img src="resource/template/theme-daynight/screenshot.png" width="3000px"/>                            | <img src="resource/template/theme-hotaru/screenshot.png" width="1500px" /> |
| <div align="center"><b>Neko Mdui <a href="https://github.com/MikoyChinese">@MikoyChinese</a></b></div> |      <div align="center"><b>AngelKanade <a href="https://github.com/adminsama">@adminsama</a></b></div>         |<div align="center"><b>ServerStatus <a href="https://github.com/unclezs">@unclezs</a></b></div> |
| ![Neko Mdui](resource/template/theme-mdui/screenshot.png)                                              |        ![AngelKanade](resource/template/theme-angel-kanade/screenshot.png)            | ![默认主题魔改](resource/template/theme-server-status/screenshot.png)       |

You can change the dashboard language in the settings page (`/setting`) after the dashboard is installed.

## Note
半透明模式的开关默认隐藏，打开半透明模式需要在自定义代码中添加:

```bash
<script>
	// server-status 默认开启分组
    localStorage.setItem("showGroup", true);
    // server-status 默认打开半透明模式
    localStorage.setItem("semiTransparent", true); 
</script>
```
