# 账号服务 v1

插件 ID 为 `raylea.mihoyo-accounts`。通过宿主 `plugin.call` 调用服务 `accounts`、版本 `1`。

## 边界

默认所有插件可信。账号插件维护允许调用的插件列表及账号用途。宿主来源上下文提供调用者和原始主体，`params` 不接受调用者自行声明的管理员标志。

CK、SToken、LToken、Cookie Token、AuthKey、登录会话材料不出现在服务结果或普通配置中。持久化前由本插件 AES-256-GCM 加密，再写入自身 secret 命名空间。主密钥置于插件数据目录，由 Windows 用户范围 DPAPI 或口令包装，不保存在宿主数据库中。

登录页面与本人私聊可显示官方二维码 PNG；这是完成扫码所需的临时登录入口。普通服务结果不包含二维码、原始 URL 或 ticket，二维码不写入数据库或文件。

## 方法

`public.execute` 接收 `game`、公开 `uid`、国服 `region`，只代理固定的 `profile` 操作。必须有账号所有者本人私聊主动开启的公共查询账号，且调用游戏仍在全局/该账号的允许名单中；不返回出借账号引用、账号所有者或凭据。公共引用不授予私人查询权限，定时来源不能使用此接口。

本人私聊“共享查询账号 开启 [序号] [每日额度] [原神,星铁,绝区零]”选择用途；默认每日 100、上限 1000，省略游戏时为已绑定的游戏。“共享查询账号 关闭 [序号]”撤销。管理页只能通过 `accounts.public.disable` 关闭，不代替所有者开启；最多 128 个公共账号。用途、索引、当日用量和短间隔均加密保存；北京时间零点重置额度，关闭/重开不返还当日用量。请求先持久扣次，单账号至少间隔五秒，再发一次官方请求；验证码/限流错误不换账号重试。迁移恢复清空公共用途，需要本人在目标实例重新开启。

| 方法 | 输入 | 返回 |
| --- | --- | --- |
| `list` | 空对象；管理来源可选 `subject` 或 `page` | 当前主体可用的账号摘要、角色和凭据状态；管理页按账号页返回 `next_page` |
| `roles` | `account_ref`，可选 `game` | 该主体有权使用的绑定角色 |
| `select` | `account_ref`、`role_ref` | 为账号主体选择该游戏的默认角色 |
| `execute` | `account_ref`、`role_ref`、`operation`、对象 `input` | 指定操作的无凭据业务结果 |
| `delegation.create` | `account_ref`、`role_ref`、`task_id`、`operation`、`days`（1–90） | 显式创建只读定时委托，返回 `delegation` |
| `delegation.revoke` | `account_ref`、`delegation_ref` | 撤销本人或管理来源指定的、属于调用插件的委托 |

定时委托开放三游戏 `note`、`sign` 和原神/绝区零 `gacha`；签到委托创建还必须带 `write_confirmed=true`。每个账号最多 16 项有效委托，同一调用插件及 task_id 更新同一委托。创建继续校验聊天主体或正式管理来源，不能从调度事件创建。委托把调用插件、task_id、角色、操作、到期时间一起加密保存在账号记录中。

调度 `execute` 必须携带 `delegation_ref`，来源必须是宿主的 `scheduler.trigger` / `scheduler` / `scheduler.internal`，且宿主 `origin.task_id` 与委托匹配；忽略任何自报用户。`note/sign` 要求 `input` 为空，最少五分钟间隔；`gacha` 仅接受该游戏允许的 `gacha_type`、字符串 `end_id`（1–19 位数字）、整数 `page`（1–10000），至少一秒间隔。抽卡不得传入 UID、区服、URL、AuthKey 或其他额外字段；账号身份与认证材料仍由提供者取得。每次准入重新检查账号、角色、调用者与有效期，持久写入准入时间后才发起上游请求。普通消息不能借此委托跨主体查询。错误使用 `plugin.account_delegation_denied`、`plugin.account_delegation_throttled`。删除账号或撤销调用插件授权即停止后续准入；迁移恢复不恢复有效委托，需在目标实例重新开启任务。

operation 由插件内固定注册表定义，不接受调用方提供任意 URL、Cookie、Authorization、Host 或重定向目标。

三游戏 `sign_info`/`sign_rewards` 只读查询官方游戏签到状态/当月奖励，`sign` 为显式签到。`execute` 调用 `sign` 必须带顶层 `write_confirmed=true`，并继续校验账号所有者、角色与调用插件；现有体力委托不包含签到权限。先读取状态，已签到不再提交；首次绑定提示在官方应用完成首次签到；需要验证码时返回挑战状态，不自动代解或重复 POST。活动签到认证失败使用 `plugin.upstream_sign_unavailable`，不把正常 CK 标记失效。返回只包含状态与奖励资料，不转发验证码 challenge/gt 或响应头。

原神/星铁 `growth_detail` 读取养成计算器的角色/技能详情，输入为 `character_id`；`growth_compute` 为只读材料试算，输入为 `character_id`、`current_level`、`target_level`，可选 `weapon_id/current_weapon_level/target_weapon_level` 及 `skills`（最多 32 项 `id/current_level/target_level`）。等级必须为合法整数且目标不低于当前；角色/武器上限原神 90、星铁 80，技能上限原神 10、星铁 15。详情身份使用已授权角色的 UID/区服，计算参数按固定结构重新组装，只返回材料与计算业务字段；不接受任意 body，不执行升级或消耗材料，也未授予定时委托此操作。

只读业务还包括三游戏 `monthly`，原神 `theater`、`hard_challenge`、`tcg`、`tcg_cards`、`tcg_decks`，星铁 `peak`、`rogue`、`swarm`、`gears`、`unknowable`、`divergent`、`currency_wars`、`periodic`，绝区零 `deadly`、`holo_boss`、`tower`、`void_front`、`void_front_summary`、`hollow_zero`、`hollow_zero_detail`、`lost_void`、`zenkov`、`zenkov_detail`、`exploration`。每项只返回已登记业务字段，并继续递归拒绝凭据字段和值。

`monthly` 可选 `month`：原神为 1–12，星铁/绝区零为 YYYYMM，省略读取接口默认月份。期数型挑战使用 `schedule_type=1/2`；星铁 `rogue` 使用 1/2/3（默认 3）。七圣牌库固定读取角色牌及行动牌的可用列表。身份查询参数按官方端点固定选择 `role_id/server`、`uid/region` 或 `bind_uid/bind_region`，调用方不能覆盖。原神札记固定访问 `https://hk4e-api.mihoyo.com/event/ys_ledger/monthInfo`，其他扩展端点仍位于已限定的官方主机。

`genshin.gacha`、`starrail.gacha`、`zzz.gacha` 是国服抽卡记录分页读取，input 为 `gacha_type`、可选 `end_id`（默认字符串 `0`）及 `page`（默认 1）。每页最多 20 条；返回 data 的 `list`、`next_end_id`、`has_more`、`timezone`、`lang`。列表只含标准抽卡记录字段，不含链接或 AuthKey；返回 UID 必须与所选角色一致。原神支持 100/200/301/302/400/500，星铁支持 1/2/11/12/21/22，绝区零支持 1001/2001/3001/5001/12001/13001，并转换为档案池类型 1/2/3/5/102/103。

账号插件通过固定官方端点换取 AuthKey，最多在内存缓存 128 项、每项 30 分钟，不落盘、不跨插件传输。分页端点不发送 CK/SToken。明确 AuthKey 失效时重新换取并重试一次，仍失败返回 `plugin.upstream_gacha_expired`；星铁在官方仍拒绝新换取 AuthKey 时返回 `plugin.upstream_gacha_unavailable`。两者均不使 CK 失效；海外角色返回 `plugin.account_region_unsupported`。返回官方时区值，包括绝区零当前返回的 0，不强制按国服改为 8 或转换时间字符串。

调用者须同时出现在全局 `allowed_plugins` 与账号 `allowed_callers` 中；其 `games` 进一步限制可用角色和操作。聊天来源必须与账号四字段主体完全相同，不能提交 `subject` 替换。由宿主认证的 `management.action` 来源可管理已授权账号；计划任务仅可按前述有期委托使用指定角色和操作。业务 DTO 使用操作级顶层字段名单，递归拒绝凭据字段与已知凭据值；上游结构不兼容时返回失败。

默认角色由账号插件按主体与游戏加密保存。主体查询 `list` 时通过 `default_roles` 返回仍有权限且仍存在的选择；删除、撤销或角色变化后不返回过时选择。游戏插件不保存另一份 CK 默认角色状态。

扫码凭据解析支持明确名称的 SToken/LToken，以及 app 扫码接口的单个无名称根 token；后者只作为候选，必须经固定官方 SToken 换取接口验证后才可绑定。未知的明确 token 名称与多 token 歧义不使用首项回退。已知 Set-Cookie 凭据字段只在账号插件内消费，账号 ID 不一致时拒绝保存。

## 管理动作

### 账号检查、批量维护与统计

`accounts.check` 接收 `account_ref`，通过固定官方角色绑定接口检查已保存的登录凭据，不换取新 Token、不覆盖角色列表。即使没有原神角色，成功的空列表仍表示此次账号接口认证通过；这不保证所有游戏端点、SToken 或设备验证状态都可用。检查记录 `last_check={kind:check/refresh,state:available/auth_invalid/device_required/challenge_required/unavailable,code,checked_at_ms}` 与账号一起加密保存，摘要只返回这些无凭据字段。仅明确的 `plugin.upstream_auth_invalid` 把账号标为失效，网络、限流、设备或验证码错误保留原状态。重新扫码清除旧检查记录；刷新凭据成功记录独立的 refresh 检查结果。

`accounts.batch.start` 接收 `mode=check/refresh/remove_invalid/statistics`。前三种必须给出 1–128 项 `items=[{account_ref,revision}]`，引用去重并固定本批范围；刷新与移除另需 `confirm=true`。移除失效账号只处理仍为 invalid 且 revision 与选择时相同的记录，其他条目返回 skipped，不自动扩大删除范围，不因网络或设备错误而删除。此动作仅由正式管理页面调用，不向游戏服务开放。单账号 `accounts.remove_invalid` 同样需要 revision 和 confirm；既有普通移除仍为独立操作。

缺少 SToken 的账号刷新返回 `plugin.account_refresh_unavailable`，不请求上游、不改变现有登录状态；批次将其列为 skipped，不能把缺少刷新材料当成 Cookie 已失效。批量动作继续拒绝 `password` 明文字段。

`accounts.batch.step` 接收 `ref` 和从 0 开始的 `sequence`，每次用当前管理事件处理一个账号，或读取一页统计。紧接着重传上一 sequence 返回同一结果，不重复刷新或删除。每项保留成功/失败/跳过及固定错误说明；锁定账号库、操作超时或存储失败不推进游标，可显式继续。`accounts.batch.cancel` 停止剩余条目，不能撤销已完成的刷新或删除。管理页关闭停止自动推进；批次最多八个、空闲十五分钟过期，仅在内存保存无凭据结果，不持有已结束事件的存储或动作客户端。

statistics 从加密账号目录分页汇总保存账号数、完整聊天主体数、记录状态、各游戏角色数、可刷新账号数与公共查询用途数，不访问米游社、不换取凭据。最多汇总 16384 个账号和 32768 页，超限明确停止；结果标明开始/结束时间及已读取页数，这是一次分页观察，不声明为同一时刻的原子快照。统计可重新计算，没有第二份持久化“用户缓存”需要重置，也不通过刷新统计清除任何账号。

本人私聊 `检查CK [序号]` 检查指定账号；`我的CK` 显示最近检查摘要。未提供序号时沿用第一个账号。其他用户、群聊不能借序号检查他人账号。批量管理和全局统计使用原生管理页，不沿用聊天中携带明文 CK 或跨用户代发的方式。

### 加密迁移与密钥轮换

轮换时无法验证现有密钥文件/口令返回 `plugin.vault_rotation_auth_failed`，不会把仍解锁的账号库误报为锁定；达到 128 代上限返回 `plugin.vault_rotation_limit`。两种失败均不改变当前密钥。

`vault.rotate` 在账号库已解锁时轮换数据加密密钥；口令保护的库需提供 `password_ciphertext` 验证现有保护口令。新密钥先与旧密钥一起受原保护方式包装并原子写入密钥文件，再启用新写入密钥。旧密钥保留用于读取历史密文及删除留下的索引，不宣称轮换会使外部旧副本不可解密。最多保留 128 代，达到上限明确拒绝。`vault.reencrypt` 按 `page` 重加密当前活动账号、所属索引和默认角色；每页原子提交，失败可重试，重启后混合代次仍可读取。此流程只作用于账号插件自身命名空间，不扩大主程序加密范围。

`vault.export.start` 接收新的备份口令密文，生成全部活动账号和默认角色的加密快照；`vault.export.read` 按 `ref`、`offset` 读取最多 64 KiB 的加密文件片段，`vault.transfer.cancel` 丢弃任务。`vault.import.start/append/finish` 上传同格式加密文件，完成时提供备份口令密文；只允许恢复到没有活动账号的已解锁库，验证完整格式、主体/引用唯一性、账号字段和默认角色后一次重建索引并提交。密码不匹配、篡改、超限或写入失败不会部分覆盖活动库。

备份采用独立盐和 PBKDF2-SHA256 600000 次派生，再使用 AES-256-GCM 随机 nonce 和固定用途 AAD；文件只包含版本、盐、参数及认证密文，不包含明文 CK 或主密钥。客户端只上传/下载密文；恢复后重新用目标机器的账号库密钥加密数据库记录，无需复制旧 DPAPI 密钥。原主体的协议/适配器/机器人/聊天用户保持不变，不按相同字符串自动跨机器人授权。

传输任务最多 4 个、空闲 10 分钟过期，上传按顺序并允许相同片段重传，单文件最多 16 MiB、最多 4096 个活动账号；导出前还检查恢复密文批次不超过 6 MiB，超限拒绝导出，避免产生无法原子恢复的文件。锁定账号库时取消所有传输，普通聊天及游戏服务不能调用这些管理动作。此上限属于迁移文件边界，超限明确报错，不截断数据。

角色详情的属性元数据随业务结果返回：原神 `property_map`，星铁 `property_info`。`starrail.character` 增加可选 `character_ids`（1–50 个正整数），由提供者筛选已认证响应中的角色，不把未登记的查询参数传给官方；省略时兼容原角色列表。三游戏的返回结果不包含 CK 或活动会话。

`starrail.gacha_summary` 是独立的国服官方小程序摘要查询，输入 `gacha_type`（1/2/11/12/21/22，默认 11），可选 `version_id` 和 `max_id` 一并作为五星记录分页游标。返回 `coverage=five_star_and_pool_summary`、`pool`、`five_star`、`gacha_type`；不写入逐抽档案。账号插件通过固定 `common/badge/v1/login/account` 获取临时活动 Cookie，仅在内存使用，再访问固定官方 `act-api-takumi.mihoyo.com/event/rpg_gacha_record/` 下 `pool_stat` 与 `five_star_list`。该摘要不代表完整逐抽历史，不改变完整抽卡同步受限状态。

- `status`、`accounts.list`：状态和摘要。
- `login.start`、`login.poll`、`login.cancel`：创建、轮询和取消登录任务；任务引用不是凭据，任务绑定到创建主体。
- `accounts.refresh`、`accounts.remove`、`accounts.roles`：显式更新或删除账号。`accounts.roles` 每次选择一个 `game`；刷新重新换取 LToken 与 Cookie Token。
- `accounts.permissions`：提交 `account_ref`、当前 `revision` 与 `callers`；代次不匹配返回 `plugin.account_changed`。
- `vault.public_key`、`vault.initialize`、`vault.unlock`、`vault.lock`：密钥状态与口令包装。口令输入必须使用插件会话公钥加密的 `password_ciphertext`，不接受明文 password 字段。

管理请求由宿主管理会话认证。为聊天用户绑定时必须显式选择主体；聊天扫码默认绑定事件原始主体。主体由 `source_protocol`、`source_adapter`、`bot_id`、`actor_id` 共同构成，不能按相同字符串 ID 自动跨协议合并。

## 状态与失败

登录任务状态为 `waiting`、`scanned`、`bound`、`expired`、`canceled`、`failed`。任务只存内存，重启后失效；凭据成功加密持久化后才能返回 bound。

后台每五秒推进扫码、换取凭据及三游戏角色发现；后台不复用已经结束的宿主事件。页面每两秒通过新的 `login.poll` 获取状态并完成加密提交。私聊通过固定的每分钟调度事件提交并通知，用户也可用“登录状态”立即提交。未确认二维码三分钟失效；确认后另给三分钟完成保存，到期自动清理内存材料。网络及限流故障最多重试三次并退避；认证拒绝、验证要求和结构不兼容立即返回 failed，确认后的处理超时不伪装为二维码过期。失败摘要保留阶段、操作、retcode、HTTP 状态和固定诊断原因，不包含上游正文或凭据。部分游戏角色发现失败会单独显示，可在绑定后按游戏重试。再次为同主体扫码会取消尚未完成的旧任务。

摘要返回账号引用、代次、掩码账号、归属、角色、指定插件、状态、可刷新标志和更新时间。`status` 还返回可用机器人、支持操作、密钥保护方式及账号库状态。账号列表每页最多 128 项，每个聊天主体最多 32 个账号。

凭据状态为 `unknown`、`valid`、`invalid`，与密钥的 `uninitialized`、`locked`、`unlocked`、`missing` 状态分别展示。只有明确认证失效响应能使凭据失效；网络、限流或验证码不删除凭据。

游戏查询前由账号插件向固定官方 HTTPS 端点注册逻辑设备指纹，注册请求不携带账号凭据。指纹与有效期随账号加密保存，三个游戏共用；旧记录缺少指纹时在下一次查询补齐。已缓存的指纹被拒绝时最多更新并重试一次只读查询，验证码或认证失效不自动重试。服务失败仅记录固定操作名、错误码、HTTP 状态与诊断原因。

私聊命令“我的CK”“刷新CK”“删除CK”兼容 `CK`、`ck`、`Ck`、`cK`；命令前缀沿用宿主设置。

失败使用稳定的 `plugin.account_*`、`plugin.login_*`、`plugin.vault_*`、`plugin.upstream_*` code。details 只允许无凭据的操作名、上游 retcode 或需要用户处理的状态，不返回上游原始正文或带凭据 URL。

同账号刷新串行，提交前再次检查账号代次，删除后不得被迟到刷新恢复。密码/密钥丢失不自动生成新 key 覆盖现有密文。

绝区零试炼业务 DTO 包含 `climbing_tower_s4`，与其他赛季同属无凭据查询数据。

挑战提醒委托开放固定只读操作：原神 `abyss/theater/hard_challenge`、星铁 `challenge/story/boss/peak`、绝区零 `challenge/deadly/holo_boss/void_front/tower`。它们只接受空 `input`，读取接口默认本期范围；最少五分钟间隔，仍绑定角色、操作、调用插件、宿主 task_id 和有效期。不能通过该委托查询其他角色、任意历史参数或执行写操作。

三游戏 `monthly` 也可创建只读定时委托，仅空输入读取官方默认月份，仍遵守五分钟准入间隔及角色/任务/有效期绑定。


## 社区与云游戏账号操作

三游戏 `community_status/community_posts/community_sign/community_read/community_like/community_unlike/community_share` 是账号级操作：`execute` 仅需 `account_ref`，`role_ref` 留空；仍校验原主体、账号调用者名单及调用插件的游戏范围。帖子列表固定本游戏讨论区，读帖/点赞/取消/分享只接受数字 `post_id`。签到、读帖计数、点赞/取消与分享属于显式操作，均要求 `write_confirmed=true`；状态和列表只读。所有认证请求由账号插件发送，游戏只得到米游币、任务、帖子摘要或完成结果。

社区使用独立固定请求配置（app 2.109.0、SToken/MID；状态查询使用 CK）；社区认证失败返回 `plugin.upstream_community_unavailable`，不将正常游戏 CK 标为失效。验证码和限流保留独立错误，写操作不自动重放。

原神/绝区零 `cloud_sign/cloud_status/cloud_rewards/cloud_claim` 为账号级云游戏操作。`cloud_sign` 的钱包 GET 可能发放免费时长，必须确认；`cloud_status` 仅读已保存的最近钱包结果并注明时间，不暗中触发领取。原神还支持读取未处理通知及显式确认领取。云授权配置由账号管理页 `accounts.cloud.save/clear` 提供，使用 `box`（RSA-OAEP/AES-GCM，加密用途 `account.cloud.save`）和账号 `revision`；仅保存云端 `combo_token` 密文，摘要只显示配置存在性。令牌不可用不影响普通游戏 CK，暂未提供无依据的 CK→云 Token 换取。已有相同账号重登/刷新保留云授权；删除账号清除全部当前凭据。


账号级定时委托支持 `community_run` 和 `cloud_sign`，不需要游戏角色，`role_ref` 必须为空。创建均须 `write_confirmed=true`。community_run 仅接受固定 `action=status/posts/sign/read/like/unlike/share` 和相关数字 `post_id`，最少三秒间隔；云签到空输入、最少五分钟。仍绑定调用插件、真实 task_id、账号主体与到期时间；没有自动 CAPTCHA 重试或写请求重放。


## 兑换与资产变动记录

三游戏 `redeem` 只接受 `code` 和已绑定角色，必须 `write_confirmed=true`。固定参考仅提供海外 `webExchangeCdkeyHyl`，国服返回 `plugin.account_redeem_unavailable` 并提示在官方游戏内兑换；不猜测国服接口、不把该限制当成 CK 失效。实际使用固定游戏 HTTPS 主机、角色 UID/区服和 Cookie，凭据不离开账号插件，写入不自动重放。

原神/星铁 `billing` 只读代理固定客服资产记录：原神 `crystal/primogem` 使用字符串 `end_id`，星铁 `dreams/stellar/power/relic/cone` 使用整数 `page`。`direction=produce/consume/all` 筛选变动方向。账号插件用 SToken 换取独立 `csc` AuthKey，仅在内存短期缓存；按账号、角色及用途隔离，AuthKey/原始链接不回传。原神先校验客服返回 UID 与所选角色一致。返回最多 20 条无凭据记录、下一游标/页码及覆盖边界；授权拒绝使用 `plugin.upstream_billing_unavailable`，不会判定普通游戏 CK 失效。


`genshin.blueprint_read/blueprint_compute` 为本人角色读取尘歌壶10–15位分享码和计算摆设材料。仅固定官方家具 blueprint/compute 端点，国服及原神海外明确区服；read 输入 share_code，compute 输入1–500项 {id,cnt}、ID正整数且数量1–9999。计算为只读业务，不执行游戏内放置或消耗。凭据、原始头和URL不返回调用方。


### 本人跨聊天关联

“关联账号”仅本人私聊发起五分钟会话；另一聊天身份以“申请关联 编号”登记；发起端以“关联进度 编号”核对完整目标身份，再以“完成关联 编号 目标用户ID”确认。只有完成最后确认才建立主身份到附属身份的共享，编号一次使用，过期/取消/锁库/进程退出失效。不复制 CK，不改账号原始 Owner，不允许链式关联、环路或一个附属身份关联多个主身份。双方原有账号保留。

附属身份可通过账号服务使用主身份已允许的插件和角色。列表以当前请求身份作为任务通知归属，默认角色选择分别保存；原账号的删除、共享公共查询和调用者授权继续由原始 Owner 或管理会话处理。关联本身及索引在账号插件内加密保存。`accounts.associations.list/remove` 管理查看或明确解除；聊天“关联列表”“解除关联 序号”只操作本人有关联的一项。

定时委托额外记录创建它的完整聊天身份；旧委托缺省使用原始 Owner。执行前检查该身份仍有访问权，解除关联后新执行被拒绝。数据库密钥重加密包含关联记录与附属默认选择；账号备份恢复默认清除关联、定时委托和公共共享，需要重新确认关联。

### 设备控制

`accounts.device.status/reset` 面向管理，状态只显示设备标识的掩码、指纹是否存在及到期时间；reset 要求账号版本号和 confirm，可自动生成新标识或使用有效的设备标识，清除旧指纹。下次需要指纹的业务从官方重新获取。现有 CK 不回显，不自动重试游戏写操作。


### 海外 HoYoLab 账号

账号增加 `realm=cn/os`，旧记录缺省cn；相同数字通行证ID在不同地区不合并。国服继续米游社扫码。海外管理页使用浏览器混合加密输入本人 HoYoLab Cookie（`account.overseas.import`用途），仅接收白名单账号ID、LToken、CookieToken、可选SToken/MID；凭据字段在账号进程内解析，其他字段不保存，普通管理响应和游戏插件不接收原文。

`accounts.overseas.start/step/cancel` 保存五分钟进程内导入会话，每一步发现一个游戏的官方海外角色，三步结束后加密绑定。未完成流程不写账号库；认证失败明确停止，短暂不可用可重试当前步骤。锁库、重启清除会话。正常业务使用各游戏固定海外战绩、签到、月报、养成/家具、兑换与抽卡端点，地区不匹配不发送凭据；社区米游币与国服云游戏不跨区混用。

海外 Cookie 的 v1/v2字段分别记录，SToken v2与MID可经固定官方端点刷新LToken/CookieToken，旧SToken使用固定旧版账号接口；逐抽AuthKey用CookieToken v2和MID在账号插件内换取。只有存在已验证源路径的操作才接入，未提供地区路径保持明确不支持。接口和签名参考固定 `genshin.py` 075f0e1e332f9452e544aa7dfdf265678889d6ab 与既有 Yunzai；海外真人扫码/账号/设备验证保留实测条件，不把合成验证写成真人登录成功。


`genshin.birthday_list/birthday_claim` 代理留影叙佳期活动。活动临时 Cookie 仅留在账号插件内存，list 只返回生日角色与官方图片链接；claim 要求 write_confirmed、固定数字 role_id，并先确认该角色在当前活动列表。失败不自动重放领取请求，社区活动认证失败不直接宣告普通游戏CK失效。
