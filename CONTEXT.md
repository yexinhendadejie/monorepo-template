# 项目上下文

## 当前目标

首期只完成“机台原始数据 → 解析 → 人能看的 Recipe Body 页面”。这条读取链路优先于编辑、保存、版本对比，不实现机台文件生成或下发。

## 已确认决策

- `apps/` 放项目，`packages/` 放公共包。
- 前端：React、TypeScript、Tailwind CSS、Iconify Tailwind 集成、shadcn/ui、Vite。
- 后端：Go、Viper、Air、Gin、GORM、Casbin；数据库为 PostgreSQL。
- `apps/mockserver` 使用 Node，模拟机台列表、配方目录与原始文件读取。
- 第一批数据为 ABMCPT01 的 `.pgs` 和 ABMCPT03 的 `.dcex`；保留原始字节，解析时保留来源、顺序和机台字段。
- Excel 是供人阅读的 Recipe Body 表达，机台文件是机器格式；不能因页面字段少而丢弃原始参数。
- 单人本地开发，暂不做真实身份登录。未知单位编码或字段语义不推测，显示原值并提示未确认。
- 设计稿来源：`/Users/yexinhendadejie/.vetta/workspace/test-design/recipe-workbench.vetd`。迁移其暖灰底、白色详情面板、蓝色强调和表格工作台布局，替换静态数据与无效控件。
- PostgreSQL Compose 放在 `/Users/yexinhendadejie/Documents/docker/pgsql`。本期只读转换链路不依赖数据库，不创建业务表。
- 首期 JavaScript 使用 npm workspaces 和单一根锁文件；Go 使用根模块，应用与解析公共包共享该模块。

## 后续阶段保留的业务决策

标准配方独立存在，通过映射关联机台配方。首次可从指定机台导入，测试项自动建议匹配、人工确认。优先编辑测试规格，普通保存只更新草稿，主动建版生成不可变快照。写入只保存 PostgreSQL，不回写机台。这些属于后续阶段，不作为首期已实现功能。

## 待业务确认

- `.dcex` 的测量单位、Range 编码、Bias 含义和特殊值的厂商定义。
- `.pgs` 函数与测试项的启用/判定标志完整语义，以及与 Excel 测试编号的对应规则。
- 不同设备是否存在更多文件格式；本期只支持已提供的两类样例。
