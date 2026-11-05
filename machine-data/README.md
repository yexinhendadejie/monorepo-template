# machine-data

云盘同步下来的机台原始配方，只在本机存在，不进入版本库。

- `make sync` 从 FTP 拉取到本目录，按机台分子目录保存。
- `make sync-dry` 只列出将要下载的文件，不写盘。
- `make sync-prune` 额外删除云盘上已不存在的本地文件。

除本文件外，本目录内容被 `.gitignore` 忽略。写入前会校验路径，拒绝落到本目录之外。

机台模拟服务（`apps/mockserver`）默认读本目录，每个子目录是一台机台，目录内的扩展名决定配方格式（`.pgs` / `.dcex`）。部署栈以只读方式把本目录挂进 mock 容器。测试不依赖这里的真实数据，用的是 `apps/mockserver/testdata/` 下的合成样本。
