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
		"genshin.monthly":         {game: "genshin", host: ledgerHost, path: "/event/ys_ledger/monthInfo", identity: "bind", inputs: words("month"), defaults: map[string]string{"month": ""}, fields: words("uid region nickname current_month data_month month month_data day_data optional_month data_last_month last_month_data")},
		"genshin.tcg":             {game: "genshin", path: "/game_record/app/genshin/api/gcg/basicInfo", fields: words("level nickname avatar_card_num_gained avatar_card_num_total action_card_num_gained action_card_num_total challenge_basic covers replays avatar_card_num action_card_num avatar_card_list action_card_list challenge_list level_info card_list stats is_unlock")},
		"genshin.tcg_cards":       {game: "genshin", path: "/game_record/app/genshin/api/gcg/cardList", defaults: map[string]string{"limit": "999", "offset": "0", "need_action": "true", "need_avatar": "true", "need_stats": "true"}, fields: words("list card_list avatar_card_list action_card_list stats total avatar_cards action_cards is_last next_offset")},
		"genshin.tcg_decks":       {game: "genshin", path: "/game_record/app/genshin/api/gcg/deckList", fields: words("deck_list list total")},
		"starrail.monthly":        {game: "starrail", host: webHost, path: "/event/srledger/month_info", identity: "uid", inputs: words("month"), defaults: map[string]string{"lang": "zh-cn", "month": ""}, fields: words("uid region nickname current_month data_month month month_data day_data optional_month data_last_month last_month_data")},
		"starrail.peak":           {game: "starrail", path: "/game_record/app/hkrpg/api/challenge_peak", inputs: words("schedule_type need_all"), fields: words("schedule_id begin_time end_time star_num max_floor battle_num has_data all_floor_detail groups challenge_peak_records challenge_peak_best_record_brief has_more_boss_record challenge_peak_best_record challenge_peak_rank_list challenge_peak_info")},
		"starrail.rogue":          {game: "starrail", path: "/game_record/app/hkrpg/api/rogue", inputs: words("schedule_type need_detail"), fields: words("role basic_info current_record last_record records record_list has_data is_unlock")},
		"starrail.swarm":          {game: "starrail", path: "/game_record/app/hkrpg/api/rogue_locust", inputs: words("need_detail"), fields: words("role basic basic_info records record_list detail has_data is_unlock")},
		"starrail.gears":          {game: "starrail", path: "/game_record/app/hkrpg/api/rogue_nous", inputs: words("need_detail"), fields: words("role basic basic_info records record_list detail has_data is_unlock")},
		"starrail.unknowable":     {game: "starrail", path: "/game_record/app/hkrpg/api/rogue_magic", fields: words("role basic_info normal_record_list custom_record_list")},
		"starrail.divergent":      {game: "starrail", path: "/game_record/app/hkrpg/api/rogue_tourn", inputs: words("need_detail"), fields: words("role basic basic_info records record_list detail has_data is_unlock overview weekly_record permanent_record challenge_record normal_detail cur_week_detail last_week_detail")},
		"starrail.currency_wars":  {game: "starrail", path: "/game_record/app/hkrpg/api/grid_fight", fields: words("role basic basic_info records record_list detail has_data is_unlock overview statistics grid_fight_brief grid_fight_archive_list")},
		"starrail.periodic":       {game: "starrail", path: "/game_record/app/hkrpg/api/periodic_act", fields: words("role basic_info records record_list data detail has_data is_unlock act_list act_info_list grid_fight_current_season_id rogue_tourn_current_season_id")},
		"zzz.monthly":             {game: "zzz", host: webHost, path: "/event/nap_ledger/month_info", identity: "uid", inputs: words("month"), fields: words("uid region current_month data_month month_data optional_month")},
		"zzz.deadly":              {game: "zzz", path: "/event/game_record_zzz/api/zzz/hadal_mem_detail_v2", identity: "uid", inputs: words("schedule_type"), fields: words("start_time end_time rank_percent list has_data total_score total_star zone_id total_max_score room_max_score has_hard hard_list hard_rank_percent")},
		"zzz.holo_boss":           {game: "zzz", path: "/event/game_record_zzz/api/zzz/holo_boss_detail", identity: "uid", inputs: words("schedule_type"), fields: words("start_time end_time list unlock refresh_time")},
		"zzz.tower":               {game: "zzz", path: "/event/game_record_zzz/api/zzz/climbing_tower_detail", identity: "uid", fields: words("climbing_tower_s1 climbing_tower_s2 climbing_tower_s3 climbing_tower_s4")},
		"zzz.void_front":          {game: "zzz", path: "/event/game_record_zzz/api/zzz/void_front_battle_period_detail", identity: "uid", inputs: words("schedule_type"), fields: words("has_detail_record void_front_battle_detail")},
		"zzz.void_front_summary":  {game: "zzz", path: "/event/game_record_zzz/api/zzz/void_front_battle_period_detail", identity: "uid", defaults: map[string]string{"schedule_type": "1"}, fields: words("void_front_battle_detail void_front_id has_detail_record")},
		"zzz.hollow_zero":         {game: "zzz", path: "/event/game_record_zzz/api/zzz/abyss_abstract", fields: words("abyss_level abyss_point abyss_duty abyss_talent abyss_collect abyss_nest abyss_throne abyss_unlock refresh_time")},
		"zzz.hollow_zero_detail":  {game: "zzz", path: "/event/game_record_zzz/api/zzz/abyss_detail", defaults: map[string]string{"type": "3"}, fields: words("abyss_throne_max abyss_nest_max records list abyss_unlock unlock has_data")},
		"zzz.lost_void":           {game: "zzz", path: "/event/game_record_zzz/api/zzz/abysss2_abstract", identity: "uid", fields: words("abyss_level abyss_point abyss_duty abyss_talent abyss_collect abyss_nest abyss_throne abyss_unlock refresh_time lost_void battle_info statistics abyss_max abyss_task abyss_task_force_investigation_max special_mission unlock")},
		"zzz.zenkov":              {game: "zzz", path: "/event/game_record_zzz/api/zzz/zenkov_abstract_info", identity: "uid", fields: words("collect_total_value big_red_num millions_evacuations refresh_time abyss_duty abyss_unlock season_unlock season_data map_list max_rank is_show_percent collection_data")},
		"zzz.zenkov_detail":       {game: "zzz", path: "/event/game_record_zzz/api/zzz/zenkov_detail", identity: "uid", fields: words("map_list record_list has_data")},
		"zzz.exploration":         {game: "zzz", path: "/event/game_record_zzz/api/zzz/exploration_detail", identity: "uid", fields: words("list exploration_list area_list total_count completed_count exploration_detail area_collections cat_notes")},
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
