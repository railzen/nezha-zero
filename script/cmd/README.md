这里存放着所有关于哪吒监控官方安装脚本的文件。

如何贡献：
1. 仅更新 `template.sh`，不要编辑 `install.sh` 或者 `install_en.sh`，因为它们是生成的文件。如果你需要文本输出，请使用 Go 模板（比如 `{{.Placeholder}}`）。
2. `template.sh` 必须兼容 POSIX sh，可以使用 <https://www.shellcheck.net/> 确认。
2. 如果你在 `template.sh` 添加了任何模板，请同步更新 `locale.json`。
3. 运行 `make`。如果不能运行，请检查是否安装了 `go` 和 `make`。

对于其它没有本地化的脚本（比如 Windows 和 macOS 的），请直接更改。

---

Here lies everything related to the official Nezha installation script.

How to contribute:
1. Update `template.sh` only. Do not edit `install.sh` or `install_en.sh`, for they're generated files. Use Go templates (e.g. `{{.Placeholder}}`) if printing text is needed.
2. `template.sh` must be POSIX-compliant. To confirm it, you can use <https://www.shellcheck.net/>.
3. Update `locale.json` if any template is added in `template.sh`.
4. Run `make`. Remember to have `go` and `make` installed.

For other scripts that don't have localization (like the Windows or macOS one), edit them directly.
