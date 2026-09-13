# 控制台与运营功能使用说明

本次在 new-api 源码上完成七项补充功能，保留 QuantumNous 的项目标识与授权信息。邀请返利、两级团队收益、延期结算、Token 看板和用户邮箱列的说明见 [REFERRAL_REBATES.md](REFERRAL_REBATES.md)。

## 功能入口

| 功能 | 入口与行为 |
| --- | --- |
| 公告弹框 | 用户进入控制台后逐条展示未读公告，仅显示正文、附加信息和发布时间，保留关闭按钮。支持首页 Notice 和「系统设置 → 公告」中的公告；已展示记录保存到账号，重新登录或换设备后读取同一记录。 |
| 模型独立重试 | 「系统设置 → 模型与路由 → 路由可靠性 → 模型独立重试次数」。填写 JSON，按客户端原始模型名称精确匹配。 |
| 自动禁用渠道恢复 | 同一设置页启用定期测试，选择「仅检查被自动禁用的渠道」，并开启「成功后重新启用」。健康检查成功后恢复渠道及可用能力。 |
| 错误关键词禁用 | 同一设置页的「失败关键词」，每行一个，匹配失败提示中的任意子串，不区分英文大小写；需开启全局自动禁用和渠道自身的自动禁用。 |
| 用户重试日志 | 用户使用记录和令牌日志只显示本次请求最后一次成功或失败；管理员仍能查询所有中间错误。 |
| 模型广场统计 | 同一次请求的内部重试只发布最后一次实际尝试的状态、延迟、首 Token 延迟和吞吐；跨分组重试归属最后使用的分组。 |
| 充值与返利邮件 | 配置 SMTP 后，钱包充值成功通知充值用户；一级、二级奖励实际到账时分别通知获奖用户。 |
| 唤醒与批量管理 | 「用户管理 → 唤醒老用户」在有勾选时发送给当前表格选中的用户，没有勾选时发送给有权管理的全部已注册用户。收件范围在打开弹框时固定，并显示在弹框中。勾选用户后也可批量启用、禁用或发送邮件。 |
| 请求诊断 | 管理员在使用记录详情中查看异常请求的连接信息、上游响应头等待时间、流事件与字节数、超时和保活配置、客户端类型及上游请求 ID。新增诊断仅对管理员可见。 |

## 使用细节

### 2026-09-13 人工测试反馈

公告继续复用共享 `Dialog` 与 `RichContent`，隐藏重复的标题，移除内部重复滚动区域。用户管理的顶部邮件按钮直接读取与批量操作相同的表格选中行；弹框打开后保留收件快照，避免后台列表刷新改变发送范围。

`client_gone` / `context canceled` 表示网关观察到下游取消，单凭这一信息不能确定是用户主动停止、客户端超时还是中间代理断开。网关关闭上游响应流后，上游也可能记录相同取消错误。应结合请求 ID、客户端类型、响应头等待时间、距最后上游数据的时长、各层超时配置及客户端/代理日志排查。

新增 `other.admin_info.request_diagnostics`，只为异常请求记录连接诊断；普通用户接口继续移除整个 `admin_info`。该字段不保留请求正文、Cookie、完整请求头或带查询参数的上游 URL，错误文本经过脱敏和长度限制。关闭上游响应体导致的读取错误不作为独立上游故障记录。无计费用量时的文字改为“未获得可用的计费信息，本次未扣费”。诊断只对更新后的新请求生效，旧日志不补填；本次未修改数据库结构或计费计算。

本轮验证：用户管理、公告和使用记录相关的 15 个前端测试文件、88 项测试通过；9 个 TypeScript 变更文件 lint、保护版权头的格式处理和类型检查通过。29 个新增文案键已补齐七种语言。后端 `go test ./relay/channel ./relay/helper ./relay/common -count=1` 中，既有 HTTP/2 重试用例首次出现连接重置导致的断言失败；单独复跑及完整 `./relay/channel` 复跑均通过。新增诊断用例覆盖客户端取消传播、上游读取截断、超时与取消区分和正常结束不额外记录诊断。计费专项、日志权限投影、最终重试日志控制器测试和 `go build ./...` 通过。

```powershell
go test ./relay/channel ./relay/helper ./relay/common -count=1
go test ./service -run 'Test.*(TextQuota|TextOther|TextTool|QuotaSaturation|BillingUsage)' -count=1
go test ./model -run '^(TestLogOther.*|TestLogFormatting.*|TestFormatUserLogs.*|TestTaskPluginLogVisibility.*|TestLegacyLogOther.*|TestLegacyRejectReason.*)$' -count=1
go test ./controller -run '^(TestRelayRetryLogsExposeOnlyFinalOutcomeToUsers|TestHTTPRelayRespectsModelRetryLimitsAndFinalLog)$' -count=1
# web 目录
bun run test src/features/users/components/__tests__ src/components/console-announcement-dialog src/features/usage-logs/components/__tests__
bun run typecheck
```

模型重试示例：

```json
{
  "model-a": 2,
  "model-b": 3,
  "no-retry-model": 0
}
```

`2` 表示首次请求失败后最多再试 2 次，即最多 3 次请求；允许 0–10 的整数。单独配置的上限覆盖整个请求，切换分组不会获得额外次数。未配置的模型保留原有全局设置和跨分组重试行为。普通转发和任务提交均生效，渠道的模型映射不会改变匹配名称。

关键词配置原先已有入口。本次修复了「错误不允许重试」时提前退出、导致关键词规则被跳过的问题。状态码规则与关键词规则均可触发禁用，仍保留原有开关和渠道权限边界。

自动恢复缺陷已通过真实本地上游请求复现并修复：多密钥全部被自动禁用时，原逻辑无法选出测试密钥，因此没有真正发起健康检查。现在允许探测自动禁用的密钥，跳过手动禁用的密钥；随机和轮询模式均轮换探测，缓存刷新保留恢复进度。已经启用的渠道继续测试可用密钥。若「成功后重新启用」关闭，健康检查只报告结果。

公告有稳定 ID 时按 ID 识别；没有 ID 时按内容和发布日期等字段识别。修改无 ID 公告的内容会视为新公告。首次使用此功能时，当前尚无账号已读记录的公告会展示。

新的日志可见性规则从升级后的请求开始生效，历史日志不回填。最终渠道失败后，即使后续渠道选择失败，仍保留最后一次实际渠道错误。管理员查询不应用用户侧过滤。

批量启用、禁用沿用现有用户管理接口与角色限制；禁用会撤销已有登录会话。部分操作失败时，成功的用户取消勾选，失败的用户保持选中，便于重试。群发包含已禁用用户，跳过未绑定邮箱、邮箱格式无效及已删除用户。选中发送最多 500 个用户；「唤醒老用户」使用全部可管理用户范围。

## 邮件与后台任务

- 在系统设置中配置 SMTP 服务器、端口和发件地址；根据服务器要求配置账号、密码、SSL 或 STARTTLS。
- 标题最多 160 个字符，正文最多 20,000 个字符；正文按纯文本处理，保留换行并转义 HTML。每个用户单独收信，不暴露其他收件人地址。
- 充值成功与通知排队在同一个事务内完成；返利通知在奖励实际到账时排队。例如延期 3 天，则返利到账后发送奖励邮件。
- 充值邮件覆盖易支付、Stripe、Creem、Waffo、Waffo Pancake 的钱包充值、管理员补单和用户管理中的「增加余额」。订阅购买、兑换码、余额划转、扣减及覆盖余额不属于该通知范围；历史订单不追发。
- 「邮件发送」系统任务每分钟检查一次，每批最多处理 100 封；发送失败会延后重试，最多尝试 5 次。SMTP 失败不撤销充值或返利余额。结果可在系统任务列表查看。
- 没有配置 SMTP 时，充值与结算正常进行，通知保留在队列中；配置完成后由后台任务发送。发送前再次检查用户和邮箱，邮箱已变更或账号已删除则跳过旧通知。
- 多节点部署需保留运行系统任务的主节点，`NODE_TYPE` 不为 `slave`。任务和待发通知均存储在数据库中，重启后可继续处理。

重复支付回调、重复结算和同一邮件提交重试不会重复创建通知。发送任务使用逐条领取与租约避免多个正在运行的工作进程重复发送；SMTP 已接受正文后，QUIT 失败不再触发重发。SMTP 与数据库不能组成一个事务：若进程在 SMTP 接受邮件后、数据库记账前崩溃，租约恢复时仍可能重发该封邮件。

## 数据库与验证结果

首次启动自动创建 `announcement_views`、`email_notifications`，并为日志添加 `hidden_for_user`。公告账号与公告键、邮件事件键均有唯一约束。原邀请返利迁移仍保留。

使用真实数据库验证了新建、从发布版相关表结构升级、重复启动迁移和已有数据保留；日志还分别验证了独立 SQLite、MySQL、PostgreSQL 和 ClickHouse 实例。

| 数据库 | 实测版本 | 验证结果 |
| --- | --- | --- |
| SQLite | 3.50.4 | 通知、公告、渠道恢复、配置保存、日志过滤及升级通过 |
| MySQL | 8.0.45 | 同上，通过 |
| PostgreSQL | 16.13 | 同上，通过 |
| ClickHouse（独立日志库） | 25.8.33.6 | 日志新建、升级、重复迁移、用户及管理员查询通过 |

升级样本沿用邀请返利验证的 `v1.0.0-rc.36` User、TopUp、Option 相关表，日志使用新增可见性字段前的表结构。验证保留旧订单、余额、日志、索引与唯一约束。此次没有使用依赖更高版本的主数据库专有 SQL；未另行测试 MySQL 5.7.8 和 PostgreSQL 9.6。

环境：Go 1.25.1、Bun 1.3.14、Node 24.19.0，Windows。以下测试 DSN 必须指向专用可销毁数据库；模型矩阵拒绝使用已有表的数据库，并清理自身创建的表。

```powershell
# 先设置 TEST_MYSQL_DSN、TEST_POSTGRES_DSN、TEST_CLICKHOUSE_DSN
go test ./model -run '^(TestNotificationOutboxDatabaseMatrix|TestEmailDeliveryClaimsAndRecipientChanges|TestChannelHealthProbeDatabaseMatrix|TestClickHouseRetryLogVisibilityMigration)$' -count=1 -v
go test ./model -run 'Test.*(Channel|Cache)' -count=1

# 分别验证三种主数据库与同类型独立日志库
$env:TEST_MANAGE_USER_SEPARATE_LOG_DB='1'
foreach ($engine in @('sqlite','mysql','postgres')) {
  $env:TEST_MANAGE_USER_DIALECT=$engine
  go test ./controller -run '^(TestHTTPRelayRespectsModelRetryLimitsAndFinalLog|TestRelayRetryLogsExposeOnlyFinalOutcomeToUsers|TestUserEmailRecipientsAndAuthorization|TestAnnouncementViewsUseAuthenticatedAccount)$' -count=1 -v
  go test ./controller -run '^(TestUpdateModelRetrySettingValidationAndPersistence|TestChannelFailureKeywordMatching|TestPassiveChannelRecoveryEnablesHealthyChannel|TestTaskSubmissionRespectsModelRetryLimit|TestExecuteTaskSubmission.*)$' -count=1 -v
}
go test ./common ./service -run 'ModelRetry|SMTP|ShouldDisableChannel|SendEmail' -count=1
go build ./...

# web 目录；使用 Node 24 运行前端工具
bun run typecheck
bun run test src/features/dashboard src/features/wallet src/features/users src/features/system-settings/billing src/features/system-settings/models/__tests__/retry-limits.test.tsx src/components/console-announcement-dialog
bun run i18n:sync
bun run build
```

以上专项测试与构建通过。前端共 **16 个文件、86 项测试**通过；42 个变更的 TypeScript 文件通过 lint、保护版权头的格式检查。全部 68 个新增文案键（邀请返利 46 个、本次 22 个）在七种语言中完整，插值一致。所有邮件测试使用模拟发送器或本地测试 SMTP，没有向真实用户发送邮件。

另执行了 `go test ./common ./model ./service ./controller ./setting/...`：common、model、setting 通过；完整 service、controller 测试集存在以下基线失败，因此不宣称全量测试通过：

- controller 的既有认证、审计测试在 Windows 清理仍被占用的 `audit.db` 临时文件时失败；在未修改提交 `bdef11750` 上运行 `go test ./controller -run '^TestAuditDatabaseMatrix$/^sqlite$' -count=1 -v` 已复现。
- service 的既有渠道亲和缓存用例出现计数串扰，涉及 `MixedMode` 和 `UnsupportedModeKeepsEmpty`；在同一原始提交上运行 `go test ./service` 以及 `go test ./service -run '^TestObserveChannelAffinityUsageCacheByRelayFormat_' -count=1` 分别复现。

日志位于忽略目录 `.local-tests/improvements/`；原邀请返利验证日志位于 `.local-tests/referral/`。

## 复用与权限校验

公告复用 `AnnouncementDetailModal`；用户操作复用 `DataTableBulkActions`、`ConfirmDialog` 和 `Dialog`；模型设置复用 `JsonEditor` 和现有设置表单。新增组件仅承载公告已读状态、邮件表单等业务逻辑，没有新增通用 UI 替代实现。

权限检查在服务端执行，测试覆盖普通用户、未登录请求、同级管理员越权、越权批次回滚、公告账号隔离及禁用后的会话撤销。邮件审计仅记录发送范围和计数，不记录邮件正文。参考 OWASP ASVS **5.0.0** 的 V7.4.1、V7.4.2、V8.2.1、V8.2.2、V8.3.1、V16.2.5，以及 [Authentication](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)、[Session Management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)、[Authorization](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html)、[CSRF](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html) 指南；这些是本次相关控制的验证记录，不代表整站完成 ASVS 认证。

## 2026-09-13 模型广场最终尝试统计

原实现的性能计时从整次网关请求开始，成功重试时会包含前面失败渠道的耗时。现在每次尝试有独立计时，计费流程只提供当前尝试的输出 Token 数；控制器在每次尝试结束时保存不可变快照，在整个请求结束时只发布最终快照。后续选渠失败时保留最后一次实际尝试，异步发布的排队时间不会进入延迟。管理员的原始请求总耗时和各次错误日志继续保留。

例如先失败 30 秒，第二次请求耗时 7 秒、首 Token 耗时 2 秒并输出 50 Tokens：模型广场只记一次成功，延迟 7 秒、首 Token 延迟 2 秒、吞吐 10 Tokens/s。非流式吞吐以最后一次尝试总耗时计算。这里的重试指同一次网关请求内部的尝试，客户端另发的新请求仍独立统计。

统计口径对更新后的新请求生效，历史聚合数据无法还原每次尝试的原始时间，保留在原有时间窗口内。本次没有修改性能统计或日志数据库结构。

验证：`go test ./pkg/perf_metrics ./relay/common ./relay/helper -count=1`、计费相关 `./service` 专项及 `go build ./...` 通过。固定时间用例复现并修复了旧逻辑将 7 秒算作 37 秒的问题，同时覆盖流式/非流式、最终失败和先前尝试的首响应时间失效。真实本地上游的重试测试在三种主数据库及同类型独立日志库上通过，覆盖第一次失败、第二次成功、重试耗尽和跨分组归属；用户只见最终日志，管理员仍见全部日志。

前端 `bun run typecheck`、四个修改文件的 oxlint 和保留原版权头的 oxfmt 检查通过；`bun run test src/features/wallet` 共 4 个文件、10 项测试通过，覆盖手动增加余额的账单金额、支付方式和加载/空状态。

`./relay/channel` 整包中的既有 `TestUpstreamGetBody_HTTP2CannotRetryWithoutGetBody` 在 Windows 下仍有连接重置导致的失败，单独运行通过；在未修改的 **bdef11750** 独立工作区执行 `go test ./relay/channel -count=1` 复现了相同失败。因此本轮不宣称该整包全部通过，失败输出和专项结果保存在 `.local-tests/settlement-final-attempt/`。
