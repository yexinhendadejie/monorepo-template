# machine-data

云盘同步下来的机台原始配方，只在本机存在，不进入版本库。

- `make sync` 从操作系统已连接的 SMB 公盘 `\\10.128.0.21\d` 复制到本目录，按机台分子目录保存。Linux/macOS 先挂载共享，并在根目录 `.env` 设置 `SMB_SOURCE_DIR`；Windows 默认使用 UNC 路径。认证复用运行用户的系统登录状态，账号密码由公盘管理员提供。
- `make sync-dry` 只列出将要下载的文件，不写盘。
- `make sync-prune` 额外删除云盘上已不存在的本地文件。

除本文件外，本目录内容被 `.gitignore` 忽略。写入前会校验路径，拒绝落到本目录之外。

机台模拟服务（`apps/mockserver`）默认读本目录，每个子目录是一台机台，目录内的扩展名决定配方格式（`.pgs` / `.dcex`）。部署栈以只读方式把本目录挂进 mock 容器。测试不依赖这里的真实数据，用的是 `apps/mockserver/testdata/` 下的合成样本。
