# 邀请返利使用与验证说明

基于 new-api 开发，保留 QuantumNous 的项目标识与授权信息。

本文记录邀请返利功能与该阶段的验证。后续公告、模型重试、渠道恢复、日志可见性、邮件和批量用户管理的说明及最新验证结果见 [CONSOLE_IMPROVEMENTS.md](CONSOLE_IMPROVEMENTS.md)。充值和返利到账邮件也已接入，具体发送时机与 SMTP 配置见该说明。

## 使用入口

- 管理员：**系统设置 → 计费 → 邀请返利**，配置启用状态、一级比例、二级比例及延迟到账天数。沿用支付网关已有的合规确认要求。
- 用户：**钱包 → 推荐计划 → 邀请明细**，查看直接邀请用户、累计充值、直邀收益、团队收益和待到账奖励。点击团队收益可查看该用户邀请的下一级用户。
- **邀请明细 → 返利记录**显示充值用户、返利基数、比例、金额、状态、预计到账时间和实际到账时间。
- **数据看板 → 用户统计**可切换金额与 Tokens，分别查看排行和趋势；Token 数来自已有的 `token_used` 统计字段。
- **用户管理**新增邮箱列，未绑定邮箱显示 `—`，移动端卡片也显示邮箱。

## 返利规则

默认不启用，默认一级比例为 5%、二级比例为 0%、延迟为 3 天。两级比例各允许 0–100%，最多两位小数，合计不能超过 100%；二级设为 0 即关闭二级返利。延迟允许 0–365 个整天，0 表示随充值事务立即入账。

例如 A 邀请 B、B 邀请 C，一级为 5%、二级为 2%。C 实付金额折算为 100 账户额度时，B 获得 5，A 获得 2。A 的 B 团队收益统计只包含属于 A 的这 2 额度，不包含 B 自己收到的奖励。

在线充值返利按**实付金额折算的账户额度**计算，不把赠送额度当作现金支付。折算使用创建订单时的单价；Stripe、Creem 根据验签回调中的实付金额与原价比例校正折扣。例如充值到账 100、实付折算 90，5% 返利为 4.5。金额向下取整到系统最小额度单位。

管理员在「用户管理 → 增加余额」操作时，按**本次增加的额度**计算返利，也使用上述一级、二级比例及延期规则。增加余额会创建来源为 `admin` 的成功订单，增加余额、返利和通知排队在同一个事务内完成；任一步写入失败均回滚。订单保留精确的原始额度，不反推实付金额，账单的付款栏显示 `—`。累计充值及邀请明细包含这些手动增加的额度。

邀请人、比例、返利金额和到账时间在充值成功时固定。后续修改配置不改变已有返利；关闭新返利后，已有待到账记录仍按原时间结算。

到期返利进入现有 `aff_quota` 邀请奖励余额，并计入 `aff_history` 累计奖励。用户通过原有「划转」入口转入账户可用余额。「累计收益」沿用原奖励总额，包含旧的注册奖励；「返利记录」只显示本功能产生的充值返利。

邀请列表的「累计充值」显示已成功充值的到账额度。「直邀收益」「团队收益」包含待到账和已到账返利，并单独显示其中待到账金额。

## 支付与结算范围

- 已接入易支付、Stripe、Creem、Waffo、Waffo Pancake 的钱包充值完成流程、管理员补单及用户管理中的「增加余额」。
- 不对兑换码、余额划转、订阅购买发放充值返利；「扣减余额」和「覆盖余额」属于余额调整，不创建充值返利。覆盖为更高余额也不视为充值。
- 历史已成功订单不补发返利；升级时尚未完成的订单，在成功支付后按当时规则处理。旧订单没有折算快照时，使用完成时的价格补建快照。
- Stripe、Creem 管理员补单没有支付平台回执时，使用订单保存的返利基数，无法自动识别平台额外折扣。
- 本次未新增退款或已到账返利冲销流程。结算前发现原订单不再是成功状态或收款用户已删除，会取消待到账返利。

延迟按充值完成时间加 `天数 × 24 小时` 计算。现有系统任务框架约每分钟执行「邀请返利结算」，因此到期后可能有一个调度周期的延迟；服务重启后继续处理数据库中的待到账记录。多节点部署需保留运行系统任务的主节点（`NODE_TYPE` 不为 `slave`），并保持原支付合规确认有效。

充值、返利记录的创建在同一数据库事务内完成。每笔订单的每一级只能产生一条返利；结算通过行锁和状态条件避免重复入账。余额达到系统上限时返利保持待到账，不影响其他用户的结算。Stripe 充值事务失败时返回 HTTP 500，允许支付平台重试。

## 数据迁移

启动迁移新增 `referral_rewards` 表和 `top_ups.referral_base_quota` 可空字段，复用现有用户邀请关系与奖励余额。无历史充值回填操作，无新的外部服务依赖。

邀请返利阶段的验证使用独立、可销毁的空数据库，覆盖全新初始化及从 **v1.0.0-rc.36** 的相关 User、TopUp、Option 表结构升级；启动迁移执行两次，并额外检查返利表不会重复执行 DDL。验证既有余额、订单、订单号唯一性及订单各级返利唯一性。该阶段日志使用单独的 SQLite 数据库；后续日志可见性字段的迁移与四种日志数据库验证见上述控制台说明。

| 数据库 | 实测版本 | 结果 |
| --- | --- | --- |
| SQLite | 3.50.4 | 通过 |
| MySQL | 8.0.45 | 通过 |
| PostgreSQL | 16.13 | 通过 |

未使用依赖更高数据库版本的专有 SQL 特性。此次实测版本如上，未额外运行 MySQL 5.7.8 或 PostgreSQL 9.6。

## 验证命令与结果

使用 Go 1.25.1、Bun，以及本机捆绑的 Node 24.19.0。三库测试需事先设置 `TEST_MYSQL_DSN` 和 `TEST_POSTGRES_DSN`，且两者必须指向空的专用测试数据库；测试会删除其创建的表。

```powershell
# 项目根目录，设置好上述测试 DSN 后执行
go test ./model -run '^TestReferral' -count=1 -v
go test ./controller ./setting -run 'Referral|Recharge|ManualCompleteTopUp|WebhookEnabled' -count=1 -v
go build ./...

# web 目录
bun run typecheck
bun run test src/features/dashboard src/features/wallet src/features/users src/features/system-settings/billing
bun run i18n:sync
bun run build
```

以上命令均通过。前端测试共 **12 个文件、72 项用例**；另对本次变更的 TS/TSX 文件执行 oxlint 与保护版权头的 oxfmt 检查，通过。新增 46 个界面翻译键，七种语言完整，插值占位符保持一致。

额外执行 `go test ./model ./controller ./setting -count=1`：model 和 setting 通过；controller 的既有审计、认证等测试在 Windows 下清理仍被占用的 `audit.db` 临时文件时失败。已在未修改的原始提交 **bdef11750** 上执行以下命令复现同一问题，因此不宣称整个 controller 测试集通过：

```powershell
go test ./controller -run '^TestAuditDatabaseMatrix$/^sqlite$' -count=1 -v
```

当前工作区的专项日志位于忽略目录 `.local-tests/referral/`。新界面复用项目的 Dialog、DataTableView、DataTablePagination、LongText、ErrorState、StatusBadge 及设置表单组件。

### 2026-09-13 手动增加余额补充验证

在真实 SQLite **3.50.4**、MySQL **8.0.45**、PostgreSQL **16.13** 上重新运行 `go test ./model -run '^TestReferral' -count=1 -v`，全部通过，包括新建数据库、发布版相关表升级及重复迁移。新增场景覆盖手动增加余额的两级即时返利、三天延期、关闭返利、精确额度统计、重复结算、到账邮件，以及订单、邮件或返利写入失败时整笔回滚。

分别设置 `TEST_MANAGE_USER_DIALECT=sqlite/mysql/postgres` 和 `TEST_MANAGE_USER_SEPARATE_LOG_DB=1` 后运行 `go test ./controller -run '^(TestManageUserQuota.*|TestHTTPRelayRespectsModelRetryLimitsAndFinalLog|TestRelayRetryLogsExposeOnlyFinalOutcomeToUsers)$' -count=1 -v`，三库均通过。余额并发快照测试的同步点限定为两次初始余额读取，避免新加入的收件人查询被错误地当成新的并发请求。

上述测试仍需专用、可销毁数据库的 `TEST_MYSQL_DSN` 与 `TEST_POSTGRES_DSN`。本轮日志位于忽略目录 `.local-tests/settlement-final-attempt/`；没有发送真实测试邮件。
