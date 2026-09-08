# tcomp

[English](README.md) | [简体中文](README.zh-CN.md)

[![Release](https://github.com/evan/tcomp/actions/workflows/release.yml/badge.svg)](https://github.com/evan/tcomp/actions/workflows/release.yml)
[![Go 1.22+](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)

`tcomp` 是一个以鼠标操作为主、可直接编辑的终端文本对比工具。它并排显示两份文档，在输入时实时更新行级和词级差异，支持手动匹配行，也支持直接粘贴文本或打开 UTF-8 文件。

> 程序界面目前使用英文；本文件提供完整中文使用说明。

## 主要功能

- 左右双栏编辑，各自显示行号，纵向同步滚动。
- 相同行显示绿色，修改、插入和删除行显示红色，变化的词使用深红色背景。
- 鼠标选择文本，支持复制、剪切、粘贴，以及 OSC 52 终端剪贴板写入。
- 自动对齐不符合预期时，可通过右键手动匹配左右行。
- 支持拖动中间分割线、左右面板独立横向滚动、长行溢出提示、闪烁光标、顶部菜单和底部命令栏。
- 所有重要操作都有键盘入口，不依赖容易被终端占用的功能键。
- 提供 Linux、macOS 和 Windows 的带版本号发布包。

## 安装

### 下载发布包

从 [GitHub Releases](https://github.com/evan/tcomp/releases) 下载对应平台的压缩包，解压后将 `tcomp`（Windows 为 `tcomp.exe`）放入 `PATH`。

| 操作系统 | 架构 | 压缩格式 |
|---|---|---|
| Linux | amd64、arm64、386 | `.tar.gz` |
| macOS | amd64（Intel）、arm64（Apple Silicon） | `.tar.gz` |
| Windows | amd64、arm64 | `.zip` |

以 Linux amd64 和 `0.2.0` 版本为例：

```sh
curl -LO https://github.com/evan/tcomp/releases/download/v0.2.0/tcomp_0.2.0_linux_amd64.tar.gz
tar -xzf tcomp_0.2.0_linux_amd64.tar.gz
install -m 0755 tcomp "$HOME/.local/bin/tcomp"
tcomp --version
```

每个 Release 都包含 `checksums.txt`，解压前可校验文件：

```sh
sha256sum --check checksums.txt
```

### 使用 Go 安装

```sh
go install github.com/evan/tcomp@latest
```

需要 Go 1.22 或更高版本。

### 从源码构建

```sh
git clone https://github.com/evan/tcomp.git
cd tcomp
go build -o tcomp .
./tcomp --version
```

## 启动

```sh
tcomp
tcomp left.txt
tcomp left.txt right.txt
tcomp --version
tcomp --help
```

两个文件参数均可省略。输入文件必须是有效的 UTF-8，允许带 UTF-8 BOM。只传一个文件时右侧为空；不传文件参数时，如果存在缓存，`tcomp` 会恢复上次退出时左右两侧的文本；首次运行则从两个空面板开始。只要传入任意文件参数，就会跳过旧缓存，因此缓存内容不会覆盖显式打开的文件。

退出时，左右文本会保存到操作系统用户缓存目录中的 `tcomp/last-session.json`（多数 Linux 系统位于 `$XDG_CACHE_HOME` 或 `~/.cache`，macOS 位于 `~/Library/Caches`，Windows 位于本地应用数据缓存目录）。缓存保存的是明文，可以随时删除以恢复空白启动。

> [!IMPORTANT]
> `tcomp` 不会把修改写回输入文件。编辑内容只会保留在本地会话缓存中；如需保存到其他位置，请使用 Copy 或终端剪贴板。

## 界面说明

第一行包含 `File`、`Edit`、`Compare`、`Help` 菜单，颜色图例位于右上角。最底行提供可点击的 `Help`、`Reset`、`Replace`、`Match`、`Copy`、`Quit`，状态和错误信息显示在它的上一行。

| 标记 | 含义 |
|---|---|
| 绿色文本和行号旁竖条 | 内容相同 |
| 红色文本和行号旁竖条 | 内容有变化、插入或删除 |
| 深红色背景 | 发生变化的词或符号 |
| 黄色 `◆` | 手动匹配 |
| 黄色 `●` | 已选为手动匹配来源的行 |
| `▶` | 当前对齐行 |
| 闪烁的黄色 `▏` | 文本光标 |
| 文本边缘的 `‹` / `›` | 左侧 / 右侧还有未显示内容 |
| 状态栏中的 `Left cols …` / `Right cols …` | 长行当前可见的显示列范围 |

控制字符会显示为 `\x1B` 之类的可见转义形式，文件内容不会以终端控制序列的形式直接进入显示输出。

## 鼠标操作

鼠标是主要交互方式：

- 左键单击文本放置光标，随后可直接输入，不需要切换编辑模式。
- 按住左键拖动，可跨多行选择字符。
- 在选中文本内右键，打开复制、剪切、粘贴菜单。
- 在文本选区外右键，选择用于手动匹配的来源行。左右各选择一行后，执行 **Match selected line pair**。
- 拖动中间分割线调整左右宽度；两侧都会保留最小可用宽度。
- 普通滚轮同步纵向滚动两侧。
- 横向滚轮只滚动指针所在面板；终端不发送横向滚轮事件时，可以按住 `Shift` 使用普通滚轮。长行始终保持为一个对齐行，边缘的 `‹` / `›` 表示仍有隐藏内容，状态栏显示每侧当前可见列范围。
- 顶部菜单和底部命令都可以直接点击。

启用鼠标追踪后，如果需要使用终端自身的文本选择，请按住 `Shift` 再拖动。

## 编辑与剪贴板

编辑始终开启。普通字符插入到光标位置；Enter 换行；Backspace 和 Delete 删除。方向键每次移动一个字符或一行，Home 和 End 移到当前行首或行尾，Page Up 和 Page Down 移动一个可视页面，Ctrl+Home 和 Ctrl+End 移到文档开头或结尾。存在文本选区时，输入或粘贴会替换选中内容。在长行中移动或编辑时，当前面板会自动横向滚动，使光标始终可见。

`Ctrl+C` 和 `Ctrl+X` 会写入 `tcomp` 内部剪贴板，同时通过 OSC 52 请求终端写入系统剪贴板。`Ctrl+V` 读取内部剪贴板。终端程序无法跨平台直接读取系统剪贴板，因此外部粘贴请使用终端提供的 `Shift+Insert`、`Ctrl+Shift+V` 或右键粘贴命令。

按 `Ctrl+P` 或点击 **Replace** 可替换整侧文档。在预览界面输入或粘贴，按 `Ctrl+S` 应用，按 Esc 取消。

## 快捷键

| 输入 | 操作 |
|---|---|
| 左键单击 / 左键拖动 | 放置光标 / 选择文本 |
| 在选中文本内右键 | 打开剪贴板菜单 |
| 在其他文本位置右键 | 打开手动行匹配菜单 |
| 拖动中间分割线 | 调整左右宽度 |
| 普通滚轮 | 同步纵向滚动 |
| 横向滚轮 / Shift+滚轮 | 横向滚动鼠标所在面板 |
| 普通字符 | 插入文本或替换选区 |
| Enter | 插入新行 |
| Backspace / Delete | 删除文本或选区 |
| 方向键 | 每次移动一个字符或一行 |
| Home / End | 移到当前行首 / 行尾 |
| Page Up / Page Down | 移动一个可视页面 |
| Ctrl+Home / Ctrl+End | 移到文档开头 / 结尾 |
| Tab / Shift+Tab | 切换左右焦点 |
| Ctrl+A | 选择当前侧全部文本 |
| Ctrl+C / Ctrl+X / Ctrl+V | 复制 / 剪切 / 内部粘贴 |
| Shift+Insert 或 Ctrl+Shift+V | 在光标处粘贴外部文本或替换选区 |
| Ctrl+P | 打开整侧替换预览 |
| Ctrl+S | 应用整侧替换 |
| Ctrl+R | 重置手动匹配和选择 |
| Alt+M | 匹配通过右键选择的行 |
| Alt+F / Alt+E / Alt+C | 打开 File / Edit / Compare 菜单 |
| Alt+H | 打开帮助 |
| Esc | 关闭浮层或清除选择 |
| Ctrl+Q | 退出 |

## 对比规则

`tcomp` 使用最长公共子序列（LCS）对齐完全相同的行。未匹配区域按顺序配对，插入和删除会在另一侧显示占位。配对后的差异行再次进行词元级 LCS，只给变化的词和标点添加深红色背景。

手动匹配会成为有序锚点。同一个来源行只能参与一次匹配，锚点不能交叉。普通插入行时，未受影响的锚点会随原文移动；无法确定归属的删除或合并会移除相关锚点；整侧替换会清除全部手动匹配。

## 发布流程

[`.github/workflows/release.yml`](.github/workflows/release.yml) 会运行测试、竞态检测、vet、格式检查和七种跨平台构建。它通过 Go 链接参数写入标签版本，生成带版本号的压缩包，检查 Linux 原生二进制的版本输出，生成 SHA-256 校验文件并发布资源。

推送语义化版本标签即可创建 Release：

```sh
git tag v0.2.0
git push origin v0.2.0
```

标签可以带预发布后缀，例如 `v0.3.0-rc.1`，工作流会将其标记为 prerelease。也可以在 GitHub Actions 中手动运行并输入版本号；此时只生成可下载的 Actions 构建产物，不会创建 GitHub Release。

本地构建带版本号的二进制：

```sh
go build -trimpath -ldflags "-s -w -X main.appVersion=0.2.0" -o tcomp .
./tcomp --version
```

## 开发检查

```sh
gofmt -w *.go
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

对比和工作区逻辑与终端渲染相互独立。测试覆盖行对齐、词级差异、手动匹配校验、编辑、粘贴、剪贴板、鼠标选择、分割线调整、窄终端帮助、Unicode 宽度、控制字符显示和 CLI 版本输出。

## 当前限制

- 暂不支持直接保存文件。
- OSC 52 写入是否生效取决于终端和多路复用器配置。
- 鼠标功能需要终端转发鼠标事件。
- 行对齐使用的 LCS 内存开销与左右文档的行数乘积相关，因此当前不以超大文件为目标。
