# AffdataEdit Updater

Windows x64 与 macOS（Apple 芯片和 Intel）的文件级增量更新器，不依赖 Unity 或额外运行时。
只提供最新版本，不支持回滚到旧版本。

首次安装：从 Release 下载更新器，双击运行，它从内置的 AffdataEdit R2 地址下载完整程序，
并把自己复制到安装目录。已有安装：运行安装目录中的更新器，或在 AffdataEdit 提示有
新版本时点“立即更新”。

更新器和 AffdataEdit 互相更新：更新器只更新 AffdataEdit，不替换自己；AffdataEdit
启动时按 `updater/<平台>/latest.json` 在后台更新更新器。AffdataEdit 的清单中如果仍有
更新器本身（旧版清单），更新器会跳过它。

如需覆盖内置下载地址，可以提供 `updater-config.json`：

```json
{"manifestUrl":"https://YOUR-DOWNLOAD-DOMAIN/windows/latest.json"}
```

更新器读取清单，比对本地文件的 SHA-256，只下载缺失或不同的文件。全部下载并校验后，
提示保存并关闭编辑器，按 Enter 安装并启动新版。

也可以指定 `--install-dir` 和 `--manifest-url`。
仅使用 HTTPS，无客户端 R2 凭据。服务器发布清单前必须先完成所有文件上传。

## Windows

安装目录为更新器所在目录，包含 `AffdataEdit.exe` 和 `AffdataEdit-Updater.exe`。
不删除本地额外文件，同名自定义文件会按远程清单覆盖。替换发生错误时回滚本轮已替换文件，
并显示备份目录；强制终止时可从安装目录 `.affdata-update-*/old` 恢复备份。

## macOS

首次安装：下载并解压 `AffdataEdit-Updater-macOS.zip`，双击 `AffdataEdit-Updater`，
在终端中安装到 `/Applications/AffdataEdit/`，其中有 `AffdataEdit.app` 和更新器。
更新器未签名，第一次打开会被系统拦截，需要到“系统设置 → 隐私与安全性”点“仍要打开”。
更新器自己下载的文件没有隔离标记，之后不再需要确认。`/Applications` 需要当前用户
有写权限（管理员账户默认可写）。

`AffdataEdit.app` 整体替换：在其 APFS 克隆上替换变化的文件、删除清单中已没有的文件，
再一次重命名换入，中途中断只会留下完整的旧版或新版。需要执行权限的文件在清单中标记
`"executable": true`。

## 清单格式

```json
{
  "schema": 1,
  "version": "git-commit-sha",
  "files": [
    {"path": "AffdataEdit.exe", "sha256": "64位小写SHA256", "size": 12345},
    {"path": "AffdataEdit.app/Contents/MacOS/AffdataEdit", "sha256": "…", "size": 12345, "executable": true}
  ]
}
```

文件对象路径为清单同级的 `objects/<sha256>`；版本目录中的归档清单用于审计，
客户端应使用 `windows/latest.json` 或 `macos/latest.json`。HTTP/哈希校验失败时不会替换任何程序文件。
Unity 大型资源文件有一个字节改变也会下载整个文件，当前不是二进制差分。

## 构建与发布

`GOOS=windows GOARCH=amd64 go build -trimpath -o AffdataEdit-Updater.exe .`
`GOOS=darwin GOARCH=arm64 go build -trimpath -o AffdataEdit-Updater .`

每次 push 到 master，在 Windows 和 macOS runner 执行 `go vet` 和编译，macOS 用 `lipo`
合成通用二进制并打包为 zip，成功后自动发布 exe、zip 与 SHA256SUMS。新 Release 发布成功后删除旧 Release 和其标签，只保留最新一版。

AffdataEdit 的 CI 每次构建时取本仓库最新 Release，校验后发布到 R2 的 `updater/windows/` 和
`updater/macos/`，已安装的 AffdataEdit 据此更新更新器。
