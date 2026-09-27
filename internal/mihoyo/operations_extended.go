package mihoyo

// Endpoint paths and identity parameter names follow the pinned reference
// clients. These are read-only operations; caller input never selects a host.
func init() {
	for _, game := range []string{"genshin", "starrail", "zzz"} {
		operations[game+".sign_info"] = operation{game: game}
		operations[game+".sign_rewards"] = operation{game: game}
		operations[game+".sign"] = operation{game: game, mutates: true}
	}
	entries := map[string]operation{
		"genshin.growth_detail":   {game: "genshin"},
		"genshin.growth_compute":  {game: "genshin"},
		"starrail.growth_detail":  {game: "starrail"},
		"starrail.growth_compute": {game: "starrail"},
		"starrail.gacha_summary":  {game: "starrail"},
		"genshin.monthly":         {game: "genshin", host: ledgerHost, path: "/event/ys_ledger/monthInfo", identity: "bind", inputs: words("month"), defaults: map[string]string{"month": ""}},
		"genshin.tcg":             {game: "genshin", path: "/game_record/app/genshin/api/gcg/basicInfo"},
		"genshin.tcg_cards":       {game: "genshin", path: "/game_record/app/genshin/api/gcg/cardList", defaults: map[string]string{"limit": "999", "offset": "0", "need_action": "true", "need_avatar": "true", "need_stats": "true"}},
		"genshin.tcg_decks":       {game: "genshin", path: "/game_record/app/genshin/api/gcg/deckList"},
		"starrail.monthly":        {game: "starrail", host: webHost, path: "/event/srledger/month_info", identity: "uid", inputs: words("month"), defaults: map[string]string{"lang": "zh-cn", "month": ""}},
		"starrail.peak":           {game: "starrail", path: "/game_record/app/hkrpg/api/challenge_peak", inputs: words("schedule_type need_all")},
		"starrail.rogue":          {game: "starrail", path: "/game_record/app/hkrpg/api/rogue", inputs: words("schedule_type need_detail")},
		"starrail.swarm":          {game: "starrail", path: "/game_record/app/hkrpg/api/rogue_locust", inputs: words("need_detail")},
		"starrail.gears":          {game: "starrail", path: "/game_record/app/hkrpg/api/rogue_nous", inputs: words("need_detail")},
		"starrail.unknowable":     {game: "starrail", path: "/game_record/app/hkrpg/api/rogue_magic"},
		"starrail.divergent":      {game: "starrail", path: "/game_record/app/hkrpg/api/rogue_tourn", inputs: words("need_detail")},
		"starrail.currency_wars":  {game: "starrail", path: "/game_record/app/hkrpg/api/grid_fight"},
		"zzz.monthly":             {game: "zzz", host: webHost, path: "/event/nap_ledger/month_info", identity: "uid", inputs: words("month"), order: words("uid region"), appended: words("month")},
		"zzz.deadly":              {game: "zzz", path: "/event/game_record_zzz/api/zzz/hadal_mem_detail_v2", identity: "uid", inputs: words("schedule_type"), order: words("lang uid region schedule_type")},
		"zzz.holo_boss":           {game: "zzz", path: "/event/game_record_zzz/api/zzz/holo_boss_detail", identity: "uid", inputs: words("schedule_type"), order: words("lang uid region schedule_type")},
		"zzz.tower":               {game: "zzz", path: "/event/game_record_zzz/api/zzz/climbing_tower_detail", identity: "uid", order: words("lang uid region")},
		"zzz.void_front":          {game: "zzz", path: "/event/game_record_zzz/api/zzz/void_front_battle_period_detail", identity: "uid", inputs: words("schedule_type"), order: words("lang uid region schedule_type")},
		"zzz.hollow_zero":         {game: "zzz", path: "/event/game_record_zzz/api/zzz/abyss_abstract", order: words("lang role_id server")},
		"zzz.hollow_zero_detail":  {game: "zzz", path: "/event/game_record_zzz/api/zzz/abyss_detail", defaults: map[string]string{"type": "3"}, order: words("lang role_id server type")},
		"zzz.lost_void":           {game: "zzz", path: "/event/game_record_zzz/api/zzz/abysss2_abstract", identity: "uid", order: words("lang uid region")},
		"zzz.zenkov":              {game: "zzz", path: "/event/game_record_zzz/api/zzz/zenkov_abstract_info", identity: "uid", order: words("lang uid region")},
		"zzz.zenkov_detail":       {game: "zzz", path: "/event/game_record_zzz/api/zzz/zenkov_detail", identity: "uid", order: words("lang uid region")},
		"zzz.exploration":         {game: "zzz", path: "/event/game_record_zzz/api/zzz/exploration_detail", identity: "uid", order: words("uid region")},
	}
	for name, spec := range entries {
		operations[name] = spec
	}
}

func ChallengeDelegation(name string) bool {
	switch name {
	case "genshin.abyss", "genshin.theater", "genshin.hard_challenge", "starrail.challenge", "starrail.story", "starrail.boss", "starrail.peak", "zzz.challenge", "zzz.deadly", "zzz.holo_boss", "zzz.void_front", "zzz.tower":
		return true
	}
	return false
}
