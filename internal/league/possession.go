package league

// starterPossessionLabel resolves GC-2b's possession chip text for one
// starter, matchups' and team's shared display rule: "ON OFFENSE" when
// the starter's own NFL team is known to hold the ball right now,
// "DEFENSE ON FIELD" for a started DST when its opponent is known to
// hold the ball instead — a DST scores while its own team is on defense,
// so the DST's relevant moment is the inverse of an offensive starter's.
// Every other case — no live poller wired, no game entry for the
// starter's team, or the game's own possession not itself known — returns
// "": the chip must never render a placeholder or a negative claim
// ("not on offense"), only a positive, truthful, known state (the
// truthful-state rule every new surface follows).
func starterPossessionLabel(player Player, live LiveStatus, hasLive bool) string {
	if !hasLive {
		return ""
	}
	team := normalizeNFLAbbreviation(player.NFLTeam)
	game, ok := live.Games[team]
	if !ok || !game.PossessionKnown || game.Possession == "" {
		return ""
	}
	onOffense := normalizeNFLAbbreviation(game.Possession) == team
	if player.Position == "DST" {
		onOffense = !onOffense
	}
	if !onOffense {
		return ""
	}
	if player.Position == "DST" {
		return "DEFENSE ON FIELD"
	}
	return "ON OFFENSE"
}
