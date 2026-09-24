# 资料来源

## 登录角色图

扫码登录成功后的角色图 `templates/uid-list/` 按 Miao-Yunzai 原神插件的 html/user/uid-list 改写（GPL-3.0，见 `LICENSES/Miao-Yunzai-GPL-3.0.txt`），与 Yunzai 的 showUid 一样在一张图中按原神、星穹铁道、绝区零列出角色；页面框架与样式沿用 miao-plugin 的 common/layout/elem 与 common/common.css（MIT，见 `LICENSES/miao-plugin-MIT.txt`）中本页用到的部分，同 miao 的 1.4 倍缩放，与原神、星铁、绝区零插件的“我的uid”图相同。图片地址改为宿主渲染资源。上游标出当前使用的 UID 并提示切换命令，登录结果没有当前 UID，因此不显示；底部提示改为账号命令；角色读取失败的游戏与上游“暂无uid”一样以命令标签提示发送“刷新ck”重新读取。

每个角色显示 UID、CK 标记，以及官方角色列表中的名称与等级。

- 头像：账号保存后，每个角色请求一次官方战绩首页，读取游戏内头像（原神 `game_record/app/genshin/api/index` 的 `role.game_head_icon`，星穹铁道 `game_record/app/hkrpg/api/index` 与绝区零 `event/game_record_zzz/api/zzz/index` 的 `cur_head_icon_url`），请求与游戏插件的查询一样使用账号的设备指纹与设备；该官方图片由宿主在出图时取回。读取或取回失败时使用 miao 的通用头像 `resources/common/item/face.webp`，与游戏插件没有保存面板时相同。
- 横幅：与游戏插件的“我的uid”相同，原神与星穹铁道使用 miao 的 `resources/meta-gs/character/common/imgs/banner.webp` 与 `resources/meta-sr/character/common/imgs/banner.webp`，绝区零使用原神插件的 `resources/ZZZero/img/other/banner.png`。

图中的字体与图片取自游戏插件使用的同一固定提交，由 `scripts/bundle-assets.py` 复制到插件包的 `assets/` 随插件分发（不纳入版本库），许可同各上游：

- miao-plugin [`7f6f1c84c89102bc6b1c58c8e1b06c61f4642161`](https://github.com/yoimiya-kokomi/miao-plugin/tree/7f6f1c84c89102bc6b1c58c8e1b06c61f4642161)：`resources/common/font/tttgbnumber.woff`、`resources/common/font/HYWH-65W.woff`、`resources/common/bg/bg-hydro.webp`、`resources/common/item/face.webp`，以及上述两个横幅。
- Yunzai 原神插件 [TimeRainStarSky/Yunzai-genshin `4a2e1fb8f094b2a8f039ce0768773701718b60b9`](https://github.com/TimeRainStarSky/Yunzai-genshin/tree/4a2e1fb8f094b2a8f039ce0768773701718b60b9)：`resources/ZZZero/img/other/banner.png`。
