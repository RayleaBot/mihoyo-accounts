# 账号服务 v1

插件 ID 为 `raylea.mihoyo-accounts`。通过宿主 `plugin.call` 调用服务 `accounts`、版本 `1`。

## 边界

默认所有插件可信。账号插件维护允许调用的插件列表及账号用途。宿主来源上下文提供调用者和原始主体，`params` 不接受调用者自行声明的管理员标志。

CK、SToken、LToken、Cookie Token、登录会话材料不出现在服务结果或普通配置中；AuthKey 只经下文的 `gacha.link` 出现在私聊结果中。持久化前由本插件 AES-256-GCM 加密，再写入自身 secret 命名空间。主密钥置于插件数据目录，由 Windows 用户范围 DPAPI 或口令包装，不保存在宿主数据库中。

登录页面与本人私聊可显示官方二维码 PNG；这是完成扫码所需的临时登录入口。普通服务结果不包含二维码、原始 URL 或 ticket，二维码不写入数据库或文件。

## 方法

`public.execute` 接收 `game`、公开 `uid`、国服 `region` 与可选的 `operation`，只代理两项固定的只读操作：`profile`（省略时即此项）读取该游戏的首页，`data` 为首页；`characters` 仅限原神，在同一公共账号上先读首页、再读角色列表 `genshin.characters`，即 miao 以同一公共 CK 为他人 UID 依次读取的首页与 `character/list`，`index` 为首页，`data` 为角色列表。首页读取失败时与 `profile` 一样返回该失败，不再读取角色列表；首页之后角色列表未能读取（额度用尽或官方请求失败）时仍返回首页，省略 `data`，以 `failure` 给出该失败的 `code` 与 `message`。其他 `operation` 返回 `plugin.account_operation_denied`，不扣次；结果的 `operation` 为 `<game>.<operation>`。必须有账号所有者本人私聊主动开启的公共查询账号，且调用游戏仍在全局/该账号的允许名单中；不返回出借账号引用、账号所有者或凭据。公共引用不授予私人查询权限，定时来源不能使用此接口。

本人私聊“共享查询账号 开启 [序号] [每日额度] [原神,星铁,绝区零]”选择用途；默认每日 100、上限 1000，省略游戏时为已绑定的游戏。“共享查询账号 关闭 [序号]”撤销。管理页只能通过 `accounts.public.disable` 关闭，不代替所有者开启；最多 128 个公共账号。用途、索引、当日用量和短间隔均加密保存；北京时间零点重置额度，关闭/重开不返还当日用量。每次官方请求前先持久扣一次额度：`profile` 一次，`characters` 首页与角色列表各一次。单账号的调用之间至少间隔五秒，同一调用的角色列表紧接首页读取；验证码/限流错误不换账号重试。

| 方法 | 输入 | 返回 |
| --- | --- | --- |
| `list` | 空对象；管理来源可选 `subject` 或 `page`；宿主超级管理员（OneBot11）的聊天来源可传 `all: true` 与 `page` | 当前主体可用的账号摘要、角色和凭据状态；管理页与 `all` 按账号页返回 `next_page` |
| `roles` | `account_ref`，可选 `game` | 该主体有权使用的绑定角色 |
| `select` | `account_ref`、`role_ref` | 为账号主体选择该游戏的默认角色 |
| `uids.bind` / `uids.remove` / `uids.use` | `game`、`uid` | 不需要账号：为调用主体绑定、删除或选用该游戏的 UID |
| `uids.current` | `game`、`user_id` | 同一聊天中被 @ 的用户当前使用的该游戏 UID：`uid`，没有时为空字符串 |
| `execute` | `account_ref`、`role_ref`、`operation`、对象 `input` | 指定操作的无凭据业务结果；宿主超级管理员（OneBot11）的聊天来源可对任一允许调用插件的账号执行只读的 `<game>.monthly`，对应上游主人的“原石任务”“星琼任务” |
| `delegation.create` | `account_ref`、`role_ref`、`task_id`、`operation`、`days`（1–90） | 显式创建只读定时委托，返回 `delegation` |
| `ark.request` | `route`、对象 `body` | 以本插件保存的 ark 令牌代发请求，返回 `data`（ark 响应，已去除令牌回显） |
| `ark.status` | 空对象 | ark 令牌是否已配置：`configured` |
| `delegation.revoke` | `account_ref`、`delegation_ref` | 撤销本人或管理来源指定的、属于调用插件的委托 |
| `gacha.link` | `account_ref`、`role_ref` | 绝区零国服角色的抽卡记录链接 `url`，只接受私聊消息来源 |
| `device.bind` | 空对象 | 为调用主体开始“绑定设备”：两分钟内等待本人在私聊或群聊发送设备信息，返回将绑定账号的绝区零 `uid` 与 `account_label`；所选账号没有绝区零角色时返回 `plugin.account_not_found`，不开始等待；只接受聊天消息来源 |
| `device.unbind` | 空对象 | 清除“绑定设备”所选账号导入的设备并换用新生成的设备标识，之后该账号借用默认设备（已设置时），否则使用自动生成的设备；返回 `uid` 与 `account_label`；只接受聊天消息来源 |
| `device.default` | 空对象 | 为调用主体开始“设置默认设备”：两分钟内等待本人在私聊或群聊发送设备信息，返回空对象；只接受宿主超级管理员（OneBot11）的聊天消息来源：调用者不是超级管理员返回 `plugin.account_subject_denied`，不是聊天消息来源或调用插件无权使用绝区零时返回 `plugin.account_operation_denied` |

`gacha.link` 对应 ZZZ-Plugin 的“获取抽卡链接”：以账号 SToken 换取 `authkey`，按上游格式拼出独家频段的抽卡记录链接交给绝区零插件，由它在原私聊中发出。`authkey` 只能读取抽卡记录、一天后失效，不是账号凭据；这是它离开本插件的唯一途径。群聊、管理页与定时来源一律拒绝，国际服角色返回 `plugin.account_region_unsupported`，与上游不支持国际服一致。

定时委托开放三游戏 `note`、`sign` 和原神/绝区零 `gacha`；签到委托创建还必须带 `write_confirmed=true`。每个账号最多 16 项有效委托，同一调用插件、task_id 与 operation 更新同一委托；同一 task_id 可按不同 operation 各持一项委托（如绝区零同时检查式舆防卫战与危局强袭战的提醒），每项委托只准入自己的 operation。创建继续校验聊天主体或正式管理来源，不能从调度事件创建。委托把调用插件、task_id、角色、操作、到期时间一起加密保存在账号记录中。

调度 `execute` 必须携带 `delegation_ref`，来源必须是宿主的 `scheduler.trigger` / `scheduler` / `scheduler.internal`，且宿主 `origin.task_id` 与委托匹配；忽略任何自报用户。`note/sign` 要求 `input` 为空，最少五分钟间隔；`gacha` 仅接受该游戏允许的 `gacha_type`、字符串 `end_id`（1–19 位数字）、整数 `page`（1–10000），至少一秒间隔。抽卡不得传入 UID、区服、URL、AuthKey 或其他额外字段；账号身份与认证材料仍由提供者取得。每次准入重新检查账号、角色、调用者与有效期，持久写入准入时间后才发起上游请求。普通消息不能借此委托跨主体查询。错误使用 `plugin.account_delegation_denied`、`plugin.account_delegation_throttled`。删除账号或撤销调用插件授权即停止后续准入；

operation 由插件内固定注册表定义，不接受调用方提供任意 URL、Cookie、Authorization、Host 或重定向目标。

三游戏 `sign_info`/`sign_rewards` 只读查询官方游戏签到状态/当月奖励，`sign` 为显式签到。`execute` 调用 `sign` 必须带顶层 `write_confirmed=true`，并继续校验账号所有者、角色与调用插件；现有体力委托不包含签到权限。先读取状态，已签到不再提交；首次绑定提示在官方应用完成首次签到；需要验证码时返回挑战状态，不自动代解或重复 POST。活动签到认证失败使用 `plugin.upstream_sign_unavailable`，不把正常 CK 标记失效。返回只包含状态与奖励资料，不转发验证码 challenge/gt 或响应头。

原神/星铁 `growth_detail` 读取养成计算器的角色/技能详情，输入为 `character_id`；`growth_compute` 为只读材料试算，输入为 `character_id`、`current_level`、`target_level`，可选 `weapon_id/current_weapon_level/target_weapon_level` 及 `skills`（最多 32 项 `id/current_level/target_level`）。等级必须为合法整数且目标不低于当前；角色/武器上限原神 90、星铁 80，技能上限原神 10、星铁 15。详情身份使用已授权角色的 UID/区服，计算参数按固定结构重新组装，只返回材料与计算业务字段；不接受任意 body，不执行升级或消耗材料，也未授予定时委托此操作。

只读业务还包括三游戏 `monthly`，原神 `theater`、`hard_challenge`、`hard_challenge_popularity`、`tcg`、`tcg_cards`、`tcg_decks`，星铁 `peak`、`rogue`、`swarm`、`gears`、`unknowable`、`divergent`、`currency_wars`，绝区零 `deadly`、`holo_boss`、`tower`、`void_front`、`hollow_zero`、`hollow_zero_detail`、`lost_void`、`zenkov`、`zenkov_detail`、`exploration`。这些查询返回官方响应的 data：账号插件递归删除名称含 token、cookie、password、authkey、authorization、ticket 的字段，值中出现当前账号的凭据或凭据片段时拒绝整个响应。字段选择、期数整理与只保留所查角色等业务处理由游戏插件完成。

`monthly` 可选 `month`：原神为 1–12，星铁/绝区零为 YYYYMM，省略读取接口默认月份。期数型挑战使用 `schedule_type=1/2`；星铁 `rogue` 使用 1/2/3（默认 3）；星铁 `peak` 接受 1/2/3，官方只按 1（本期）或 3（最近三期）作答，2 与 3 都以 3 请求，上期是其中第二条。星铁 `challenge`、`story`、`boss`、`peak` 默认带 `need_all=true` 读取详细记录；`input` 带布尔值 `need_all: false` 时不发送该参数，即 StarRail-plugin 的“简易”记录，`need_all` 的其他值无效。七圣牌库固定读取角色牌及行动牌的可用列表。身份查询参数按官方端点固定选择 `role_id/server`、`uid/region` 或 `bind_uid/bind_region`，调用方不能覆盖。原神札记固定访问 `https://hk4e-api.mihoyo.com/event/ys_ledger/monthInfo`，其他扩展端点仍位于已限定的官方主机。

`genshin.gacha`、`starrail.gacha`、`zzz.gacha` 是国服抽卡记录分页读取，input 为 `gacha_type`、可选 `end_id`（默认字符串 `0`）及 `page`（默认 1）。返回官方分页 data（含 `list`、`region`、`region_time_zone` 等），按上一段删除凭据字段，值中出现 AuthKey 或账号凭据时拒绝。原神支持 100/200/301/302/400/500，星铁支持 1/2/11/12/21/22，绝区零支持 1001/2001/3001/5001/12001/13001。记录校验（每页最多 20 条、UID 与所选角色一致、游标递减、池类型一致、时间与 ID 格式）、池类型转换（绝区零转为档案池类型 1/2/3/5/102/103，原神 400 归入 301）与时区换算由游戏插件完成。

账号插件通过固定官方端点换取 AuthKey，最多在内存缓存 128 项、每项 30 分钟，不落盘；除 `gacha.link` 外不跨插件传输。分页端点不发送 CK/SToken。明确 AuthKey 失效时重新换取并重试一次，仍失败返回 `plugin.upstream_gacha_expired`；星铁在官方仍拒绝新换取 AuthKey 时返回 `plugin.upstream_gacha_unavailable`。两者均不使 CK 失效；海外角色返回 `plugin.account_region_unsupported`。

调用者须同时出现在全局 `allowed_plugins` 与账号 `allowed_callers` 中；其 `games` 进一步限制可用角色和操作。聊天来源必须与账号四字段主体完全相同，不能提交 `subject` 替换。由宿主认证的 `management.action` 来源可管理已授权账号；计划任务仅可按前述有期委托使用指定角色和操作。业务 DTO 使用操作级顶层字段名单，递归拒绝凭据字段与已知凭据值；上游结构不兼容时返回失败。

默认角色由账号插件按主体与游戏加密保存。主体查询 `list` 时通过 `default_roles` 返回仍有权限且仍存在的选择；删除、撤销或角色变化后不返回过时选择。游戏插件不保存另一份 CK 默认角色状态。

`uids.current` 对应上游命令中的 @：只接受聊天消息来源，被查询的主体取来源的协议、适配器与机器人，加上 `user_id`。返回该主体当前使用的 UID（绑定的当前 UID 优先，其次是仍对调用插件可见的默认账号角色，只有一个角色时取它），不返回账号、角色引用或凭据。UID 是游戏内公开的标识，游戏插件只用它读取不需要 CK 的本地资料，不能据此使用对方的账号。

没有 CK 的用户可按上游“绑定uid”绑定任意 UID：账号插件按主体与游戏加密保存绑定的 UID 列表（每个游戏最多 20 个）与当前使用的 UID，`list` 通过 `uid_bindings` 只返回调用插件可用游戏的绑定。`uids.bind` 加入并选用一个 UID，`uids.remove` 删除，`uids.use` 选用一个已绑定或账号角色的 UID；只接受上游格式的 UID（绝区零另有 8 位），调用方只能改自己的绑定。绑定不含凭据，不授予查询该 UID 私有数据的权限；游戏插件用它作为公开展柜与本地面板的默认 UID。

扫码凭据解析支持明确名称的 SToken/LToken，以及 app 扫码接口的单个无名称根 token；后者只作为候选，必须经固定官方 SToken 换取接口验证后才可绑定。未知的明确 token 名称与多 token 歧义不使用首项回退。已知 Set-Cookie 凭据字段只在账号插件内消费，账号 ID 不一致时拒绝保存。

## 管理动作

### 账号检查、批量维护与统计

`accounts.check` 接收 `account_ref`，通过固定官方角色绑定接口检查已保存的登录凭据，不换取新 Token、不覆盖角色列表。即使没有原神角色，成功的空列表仍表示此次账号接口认证通过；这不保证所有游戏端点、SToken 或设备验证状态都可用。检查记录 `last_check={kind:check/refresh,state:available/auth_invalid/device_required/challenge_required/unavailable,code,checked_at_ms}` 与账号一起加密保存，摘要只返回这些无凭据字段。仅明确的 `plugin.upstream_auth_invalid` 把账号标为失效，网络、限流、设备或验证码错误保留原状态。重新扫码清除旧检查记录；刷新凭据成功记录独立的 refresh 检查结果。

`accounts.batch.start` 接收 `mode=check/refresh/remove_invalid/statistics`。前三种必须给出 1–128 项 `items=[{account_ref,revision}]`，引用去重并固定本批范围；刷新与移除另需 `confirm=true`。移除失效账号只处理仍为 invalid 且 revision 与选择时相同的记录，其他条目返回 skipped，不自动扩大删除范围，不因网络或设备错误而删除。此动作仅由正式管理页面调用，不向游戏服务开放。单账号 `accounts.remove_invalid` 同样需要 revision 和 confirm；既有普通移除仍为独立操作。

缺少 SToken 的账号刷新返回 `plugin.account_refresh_unavailable`，不请求上游、不改变现有登录状态；批次将其列为 skipped，不能把缺少刷新材料当成 Cookie 已失效。批量动作继续拒绝 `password` 明文字段。

`accounts.batch.step` 接收 `ref` 和从 0 开始的 `sequence`，每次用当前管理事件处理一个账号，或读取一页统计。紧接着重传上一 sequence 返回同一结果，不重复刷新或删除。每项保留成功/失败/跳过及固定错误说明；锁定账号库、操作超时或存储失败不推进游标，可显式继续。`accounts.batch.cancel` 停止剩余条目，不能撤销已完成的刷新或删除。管理页关闭停止自动推进；批次最多八个、空闲十五分钟过期，仅在内存保存无凭据结果，不持有已结束事件的存储或动作客户端。

statistics 从加密账号目录分页汇总保存账号数、完整聊天主体数、记录状态、各游戏角色数、可刷新账号数与公共查询用途数，不访问米游社、不换取凭据。最多汇总 16384 个账号和 32768 页，超限明确停止；结果标明开始/结束时间及已读取页数，这是一次分页观察，不声明为同一时刻的原子快照。统计可重新计算，没有第二份持久化“用户缓存”需要重置，也不通过刷新统计清除任何账号。

本人私聊 `检查CK [序号]` 检查指定账号；`我的CK` 显示最近检查摘要。未提供序号时沿用第一个账号。其他用户、群聊不能借序号检查他人账号。批量管理和全局统计使用原生管理页，不沿用聊天中携带明文 CK 或跨用户代发的方式。

### 账号库初始化与密钥保护

账号库只由管理页初始化，初始化时选择密钥保护方式：`dpapi` 使用当前 Windows 用户保护，插件启动时自动解锁；`password` 使用 12 到 128 字节的恢复口令，每次启动后在管理页解锁。非 Windows 系统只提供恢复口令。聊天命令不会初始化账号库：未初始化时 `扫码登录` 返回 `plugin.vault_uninitialized`，提示管理员先完成初始化。

`vault.mode` 在账号库已解锁时切换保护方式：`mode` 为 `dpapi` 或 `password`；当前为恢复口令时须提供 `password_ciphertext`，目标为恢复口令时须提供 `new_password_ciphertext`，两者都用 `vault.public_key` 返回的公钥加密。切换只用新方式重新包装同一把数据密钥并原子替换密钥文件，数据库记录不重新加密；口令错误返回 `plugin.vault_locked`，口令长度不符返回 `plugin.vault_password_invalid`，系统不支持 DPAPI 时返回 `plugin.vault_native_unavailable`，失败均不改变密钥文件。`password` 切换到 `password` 即修改恢复口令。

密钥文件可能包含早期版本留下的多代密钥，全部保留用于读取历史记录，新记录只用最新一代密钥加密。

角色详情的属性元数据随业务结果返回：原神 `property_map`，星铁 `property_info`。`starrail.character` 增加可选 `character_ids`（1–50 个正整数），由提供者筛选已认证响应中的角色，不把未登记的查询参数传给官方；省略时兼容原角色列表。三游戏的返回结果不包含 CK 或活动会话。

`starrail.gacha_summary` 是独立的国服官方小程序摘要查询，输入 `gacha_type`（1/2/11/12/21/22，默认 11），可选 `version_id` 和 `max_id` 一并作为五星记录分页游标。返回 `coverage=five_star_and_pool_summary`、`pool`、`five_star`、`gacha_type`；不写入逐抽档案。账号插件通过固定 `common/badge/v1/login/account` 获取临时活动 Cookie，仅在内存使用，再访问固定官方 `act-api-takumi.mihoyo.com/event/rpg_gacha_record/` 下 `pool_stat` 与 `five_star_list`。该摘要不代表完整逐抽历史，不改变完整抽卡同步受限状态。

- `status`、`accounts.list`：状态和摘要。
- `login.start`、`login.poll`、`login.cancel`：创建、轮询和取消登录任务；任务引用不是凭据，任务绑定到创建主体。
- `accounts.refresh`、`accounts.remove`、`accounts.roles`：显式更新或删除账号。`accounts.roles` 每次选择一个 `game`；刷新重新换取 LToken 与 Cookie Token。
- `accounts.permissions`：提交 `account_ref`、当前 `revision` 与 `callers`；代次不匹配返回 `plugin.account_changed`。
- `ark.status`、`ark.save`、`ark.clear`：ark 令牌是否已配置、保存与清除。`ark.save` 只接收用途为 `ark.token.save` 的加密输入 `box`，不接受明文令牌字段。
- `vault.public_key`、`vault.initialize`、`vault.unlock`、`vault.lock`：密钥状态与口令包装。口令输入必须使用插件会话公钥加密的 `password_ciphertext`，不接受明文 password 字段。

管理请求由宿主管理会话认证。为聊天用户绑定时必须显式选择主体；聊天扫码默认绑定事件原始主体。主体由 `source_protocol`、`source_adapter`、`bot_id`、`actor_id` 共同构成，不能按相同字符串 ID 自动跨协议合并。

## ark 令牌

ark（ark.ivny.cn）是原神与星铁插件使用的云排名与面板服务，授权查询需要机器人所有者的 ark 令牌。令牌与账号记录一样由本插件加密保存，不写入宿主明文的 secret 存储，也不交给游戏插件。

`ark.request` 只接受允许使用原神或星铁的调用插件。`route` 必须是固定清单中的路径：`auth/usage`、`panel/data`、`panel/download`、`panel/upload`、`panel/refresh`、`rank/data`、`rank/specific`、`rank/custom`、`rank/custom/specific`、`rank/self`、`rank/group`、`rank/stygian`、`pos`、`verify`、`verify/code`、`verify/user`、`ocr/profilechange/gs`、`ocr/profilechange/sr`；`body` 编码后不超过 256 KiB。本插件以 `Authorization: Bearer` 向 `https://ark.ivny.cn/<route>` 发送 POST，不跟随重定向；响应不超过 2 MiB（面板数据与下载为 8 MiB）。返回前删除所有包含令牌的字符串值与字段名。

`ark.status` 同样只接受允许使用原神或星铁的调用插件，只返回令牌是否已配置，不访问 ark；游戏插件据此决定是否提供需要令牌的功能（如 ark-plugin 的自定义排行高级筛选与帮助），未配置时它们的匿名请求直接发往 ark。

未配置令牌返回 `plugin.account_ark_unconfigured`；ark 拒绝授权返回 `plugin.account_ark_rejected`，额度用尽或过于频繁返回 `plugin.account_ark_rate_limited`，其他失败返回 `plugin.account_ark_unavailable`。

## 状态与失败

登录任务状态为 `waiting`、`scanned`、`bound`、`expired`、`canceled`、`failed`。任务只存内存，重启后失效；凭据成功加密持久化后才能返回 bound。

后台每五秒推进扫码、换取凭据及三游戏角色发现；后台不复用已经结束的宿主事件。页面每两秒通过新的 `login.poll` 获取状态并完成加密提交。私聊通过固定的每分钟调度事件提交并通知，用户也可用“登录状态”立即提交。未确认二维码三分钟失效；确认后另给三分钟完成保存，到期自动清理内存材料。网络及限流故障最多重试三次并退避；认证拒绝、验证要求和结构不兼容立即返回 failed，确认后的处理超时不伪装为二维码过期。失败摘要保留阶段、操作、retcode、HTTP 状态和固定诊断原因，不包含上游正文或凭据。部分游戏角色发现失败会单独显示，可在绑定后按游戏重试。再次为同主体扫码会取消尚未完成的旧任务。

摘要返回账号引用、代次、完整的米游社账号 ID（`account_label`，不是凭据，不打码）、归属、角色、指定插件、状态、可刷新标志和更新时间。`status` 还返回可用机器人、支持操作、密钥保护方式及账号库状态。账号列表每页最多 128 项，每个聊天主体最多 32 个账号。

凭据状态为 `unknown`、`valid`、`invalid`，与密钥的 `uninitialized`、`locked`、`unlocked`、`missing` 状态分别展示。只有明确认证失效响应能使凭据失效；网络、限流或验证码不删除凭据。

游戏查询前由账号插件向固定官方 HTTPS 端点注册逻辑设备指纹，注册请求不携带账号凭据。指纹与有效期随账号加密保存，三个游戏共用；旧记录缺少指纹时在下一次查询补齐。已缓存的指纹被拒绝时最多更新并重试一次只读查询，验证码或认证失效不自动重试。服务失败仅记录固定操作名、错误码、HTTP 状态与诊断原因。

私聊命令“我的CK”“刷新CK”“删除CK”兼容 `CK`、`ck`、`Ck`、`cK`；命令前缀沿用宿主设置。

失败使用稳定的 `plugin.account_*`、`plugin.login_*`、`plugin.vault_*`、`plugin.upstream_*` code。details 只允许无凭据的操作名、上游 retcode 或需要用户处理的状态，不返回上游原始正文或带凭据 URL。

同账号刷新串行，提交前再次检查账号代次，删除后不得被迟到刷新恢复。密码/密钥丢失不自动生成新 key 覆盖现有密文。

绝区零试炼业务 DTO 包含 `climbing_tower_s4`，与其他赛季同属无凭据查询数据。

挑战提醒委托开放固定只读操作：原神 `abyss/theater/hard_challenge`、星铁 `challenge/story/boss/peak`、绝区零 `challenge/deadly/holo_boss/void_front/tower`。它们只接受空 `input`，读取接口默认本期范围；最少五分钟间隔，仍绑定角色、操作、调用插件、宿主 task_id 和有效期。不能通过该委托查询其他角色、任意历史参数或执行写操作。

三游戏 `monthly` 也可创建只读定时委托，仅空输入读取官方默认月份，仍遵守五分钟准入间隔及角色/任务/有效期绑定。

原神 `billing` 也可创建只读定时委托，供“更新充值记录”没有客服链接时按 Yunzai 在调度任务中逐页读取：`input` 只接受 `category=crystal/primogem`、`direction=produce` 与字符串 `end_id`（1–19 位数字），至少一秒间隔，仍绑定角色、操作、调用插件、宿主 task_id 和有效期；星铁 `billing` 不开放委托。

## 社区与云游戏账号操作

三游戏 `community_status/community_posts/community_sign/community_read/community_like/community_unlike/community_share` 是账号级操作：`execute` 仅需 `account_ref`，`role_ref` 留空；仍校验原主体、账号调用者名单及调用插件的游戏范围。帖子列表固定本游戏讨论区，读帖/点赞/取消/分享只接受数字 `post_id`。签到、读帖计数、点赞/取消与分享属于显式操作，均要求 `write_confirmed=true`；状态和列表只读。所有认证请求由账号插件发送，游戏只得到米游币、任务、帖子摘要或完成结果。

社区使用独立固定请求配置（app 2.109.0、SToken/MID；状态查询使用 CK）；社区认证失败返回 `plugin.upstream_community_unavailable`，不将正常游戏 CK 标为失效。验证码和限流保留独立错误，写操作不自动重放。

原神/绝区零 `cloud_sign/cloud_status/cloud_rewards/cloud_claim` 为账号级云游戏操作。`cloud_sign` 的钱包 GET 可能发放免费时长，必须确认；`cloud_status` 仅读已保存的最近钱包结果并注明时间，不暗中触发领取。原神还支持读取未处理通知及显式确认领取。云授权配置由账号管理页 `accounts.cloud.save/clear` 提供，使用 `box`（RSA-OAEP/AES-GCM，加密用途 `account.cloud.save`）和账号 `revision`；仅保存云端 `combo_token` 密文，摘要只显示配置存在性。令牌不可用不影响普通游戏 CK，暂未提供无依据的 CK→云 Token 换取。已有相同账号重登/刷新保留云授权；删除账号清除全部当前凭据。

账号级定时委托支持 `community_run` 和 `cloud_sign`，不需要游戏角色，`role_ref` 必须为空。创建均须 `write_confirmed=true`。community_run 仅接受固定 `action=status/posts/sign/read/like/unlike/share` 和相关数字 `post_id`，最少三秒间隔；云签到空输入、最少五分钟。仍绑定调用插件、真实 task_id、账号主体与到期时间；没有自动 CAPTCHA 重试或写请求重放。

## 兑换与资产变动记录

三游戏 `redeem` 只接受 `code` 和已绑定角色，必须 `write_confirmed=true`。固定参考仅提供海外 `webExchangeCdkeyHyl`，国服返回 `plugin.account_redeem_unavailable` 并提示在官方游戏内兑换；不猜测国服接口、不把该限制当成 CK 失效。实际使用固定游戏 HTTPS 主机、角色 UID/区服和 Cookie，凭据不离开账号插件，写入不自动重放。

原神/星铁 `billing` 只读代理固定客服资产记录：原神 `crystal/primogem` 使用字符串 `end_id`，星铁 `dreams/stellar/power/relic/cone` 使用整数 `page`，可选 `page_size` 为 10–50（默认 10）。`direction=produce/consume/all` 筛选变动方向。账号插件用 SToken 换取独立 `csc` AuthKey，仅在内存短期缓存；按账号、角色及用途隔离，AuthKey/原始链接不回传。原神先校验客服返回 UID 与所选角色一致。返回一页无凭据记录（原神 20 条，星铁按页长）、下一游标/页码及覆盖边界；授权拒绝使用 `plugin.upstream_billing_unavailable`，不会判定普通游戏 CK 失效。

`genshin.blueprint_read/blueprint_compute` 为本人角色读取尘歌壶10–15位分享码和计算摆设材料。仅固定官方家具 blueprint/compute 端点，国服及原神海外明确区服；read 输入 share_code，compute 输入1–500项 {id,cnt}、ID正整数且数量1–9999。计算为只读业务，不执行游戏内放置或消耗。凭据、原始头和URL不返回调用方。

### 本人跨聊天关联

“关联账号”仅本人私聊发起五分钟会话；另一聊天身份以“申请关联 编号”登记；发起端以“关联进度 编号”核对完整目标身份，再以“完成关联 编号 目标用户ID”确认。只有完成最后确认才建立主身份到附属身份的共享，编号一次使用，过期/取消/锁库/进程退出失效。不复制 CK，不改账号原始 Owner，不允许链式关联、环路或一个附属身份关联多个主身份。双方原有账号保留。

附属身份可通过账号服务使用主身份已允许的插件和角色。列表以当前请求身份作为任务通知归属，默认角色选择分别保存；原账号的删除、共享公共查询和调用者授权继续由原始 Owner 或管理会话处理。关联本身及索引在账号插件内加密保存。`accounts.associations.list/remove` 管理查看或明确解除；聊天“关联列表”“解除关联 序号”只操作本人有关联的一项。

定时委托额外记录创建它的完整聊天身份；旧委托缺省使用原始 Owner。执行前检查该身份仍有访问权，解除关联后新执行被拒绝。

### 设备控制

`accounts.device.status/reset` 面向管理，状态只显示设备标识的掩码、指纹是否存在及到期时间；reset 要求账号版本号和 confirm，可自动生成新标识或使用有效的设备标识，清除旧指纹。下次需要指纹的业务从官方重新获取。现有 CK 不回显，不自动重试游戏写操作。

`accounts.device.import` 按 ZZZ-Plugin 的“绑定设备”导入设备，要求账号版本号与浏览器加密输入（`account.device.import` 用途）：`{"device_id","device_fp"}` 直接使用该设备标识与指纹，此后不自动换取新指纹；安卓设备信息（deviceName、deviceBoard、deviceModel、oaid、androidVersion、deviceFingerprint、deviceProduct）立即向官方登记换取指纹，之后的请求以该设备的品牌与型号标注设备。登记沿用 ZZZ-Plugin getFp 的 ext_fields：型号同时作为 deviceName 与 model，设备信息的 deviceName 作为 deviceType，aaid 与 vaid 为账号的设备标识，oaid 取设备信息中的 oaid，缺省时为设备标识；没有自己设备的账号按 ZZZ-Plugin 的默认设备（Sony XQ-BC52）登记。reset 清除导入的设备。

聊天按 ZZZ-Plugin 绑定设备：本人发送“绑定设备”（本插件的命令或绝区零插件经 `device.bind` 转来的命令）后，两分钟内本人在私聊或群聊发送的第一条以 `{` 开头的消息按上述两种格式导入，与上游按用户等待一致；“取消”结束等待，其他消息不受影响。结果回复在收到设备信息的聊天中，格式错误回复“设备信息格式错误”。提示沿用上游，建议私聊发送设备信息；群聊中绑定成功时在“绑定设备成功”后另起一行提醒撤回设备信息。设备信息不写入日志或回复。绑定的账号是持有本人当前绝区零 UID 的账号，没有时取本人唯一的账号；本人有多个账号且没有当前绝区零 UID 时要求先切换 UID。经 `device.bind` 选中的账号没有绝区零角色时返回 `plugin.account_not_found`，与上游没有绝区零 UID 时不进入绑定一致；本插件自己的“绑定设备”仍可为这样的账号绑定，提示以账号 ID 代替 UID。国际服角色返回 `plugin.account_region_unsupported`，与上游“国际服不需要绑定设备”一致。“解绑设备”（本插件或 `device.unbind`）清除同一账号导入的设备并换用新生成的设备标识，之后借用默认设备（已设置时），否则使用自动生成的设备；“绑定设备帮助”给出获取方法。出现 10041 时提示发送“绑定设备帮助”。

绝区零插件经 `device.default` 转来 ZZZ-Plugin 的“设置默认设备”：宿主超级管理员发送该命令后，两分钟内本人在私聊或群聊发送的第一条以 `{` 开头的消息须为安卓设备信息，deviceName、deviceBoard、deviceModel、androidVersion、deviceFingerprint、deviceProduct 均不可缺（与上游相同，不需要 oaid），成功回复“默认设备信息设置成功”（与上游相同，群聊中不另加撤回提醒），格式错误回复“设备信息格式错误”，“取消”结束等待。默认设备与账号同样加密保存；没有自己设备的账号（未导入安卓设备信息，也未导入设备标识与指纹）在登记指纹与发送请求时借用它，以本账号的设备标识作为 oaid，账号记录不保存这份设备。账号自己的设备始终优先；设备信息不写入日志或回复。

插件订阅聊天消息，但从不在聊天中读取或保存账号凭据：消息中出现 ltoken、cookie_token、stoken（含 _v2）、login_ticket、ltuid 或 _MHYUUID 时，回复请撤回并改用扫码登录或管理页加密导入；群聊中另提醒凭据已公开，需退出登录或修改密码使其失效。抽卡链接不是账号凭据，由各游戏插件在聊天中按上游读取。

### 海外 HoYoLab 账号

账号增加 `realm=cn/os`，旧记录缺省cn；相同数字通行证ID在不同地区不合并。国服可用米游社扫码，也可与海外一样在管理页用浏览器混合加密输入本人 Cookie（`account.cookie.import` 用途，选择国服或海外），仅接收白名单账号ID、LToken、CookieToken、SToken 与 MID；国服需要 SToken，或 LToken 与 CookieToken，海外需要 LToken，或 SToken v2 与 MID。凭据字段在账号进程内解析，其他字段不保存，普通管理响应和游戏插件不接收原文；国服导入与扫码得到的同一通行证合并为一个账号。

`accounts.import.start/step/cancel`（start 带 `realm=cn/os`）保存五分钟进程内导入会话，缺少可用凭据时先用 SToken 换取，之后每一步发现一个游戏的官方角色，三步结束后加密绑定。未完成流程不写账号库；认证失败明确停止，短暂不可用可重试当前步骤。锁库、重启清除会话。正常业务使用各游戏固定海外战绩、签到、月报、养成/家具、兑换与抽卡端点，地区不匹配不发送凭据；社区米游币与国服云游戏不跨区混用。

海外 Cookie 的 v1/v2字段分别记录，SToken v2与MID可经固定官方端点刷新LToken/CookieToken，旧SToken使用固定旧版账号接口；逐抽AuthKey用CookieToken v2和MID在账号插件内换取。只有存在已验证源路径的操作才接入，未提供地区路径保持明确不支持。接口和签名参考固定 `genshin.py` 075f0e1e332f9452e544aa7dfdf265678889d6ab 与既有 Yunzai；海外真人扫码/账号/设备验证保留实测条件，不把合成验证写成真人登录成功。

`genshin.birthday_list/birthday_claim` 代理留影叙佳期活动。活动临时 Cookie 仅留在账号插件内存，list 只返回生日角色与官方图片链接；claim 要求 write_confirmed、固定数字 role_id，并先确认该角色在当前活动列表。失败不自动重放领取请求，社区活动认证失败不直接宣告普通游戏CK失效。
