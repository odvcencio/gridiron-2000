package help

func Page() Node {
	return <main class="page guide-page help-page" id="main-content">
		<header class="page-masthead guide-masthead help-masthead">
			<div>
				<span class="signal-label"><span class="signal-mark" aria-hidden="true"></span>HELP CENTER // TASK FIRST</span>
				<p class="page-kicker">{data.runtime.league_name} · {data.runtime.mode} · {data.runtime.phase}</p>
				<h1>Help center</h1>
				<p class="guide-lede"><strong>Ask a question.</strong> Every answer says who can do it, what changes, and how to undo it.</p>
				<form class="help-search" method="get" action="/help">
					<label for="help-query">Search help</label>
					<div class="help-search__row">
						<input id="help-query" name="q" type="search" value={data.query} placeholder="Try: waiver budget, draft queue" autocomplete="off"></input>
						<button class="button button--primary" type="submit">Search</button>
					</div>
					<small>Results are sorted by match quality, always in the same order for the same words.</small>
				</form>
			</div>
			<aside class="masthead-console guide-console" aria-label="Help center runtime context">
				<div><span>Active league</span><strong>{data.runtime.league_name}</strong></div>
				<div><span>Mode / phase</span><strong>{data.runtime.mode} · {data.runtime.phase}</strong></div>
				<div><span>League timezone</span><strong>{data.runtime.timezone}</strong></div>
				<div><span>Next draft meeting</span><strong>{data.runtime.draft_at}</strong></div>
				</aside>
		</header>

		<nav class="guide-toc help-toc" aria-label="Help center sections">
			<a href="#search-results">Answers</a>
			<a href="#topic-corpus">Topics</a>
			<a href="#checklists">Checklists</a>
			<a href="#migration">Coming from another app</a>
			<a href="#glossary">Words</a>
			<a href="#recovery">When something breaks</a>
		</nav>

		<section class="guide-section guide-section--accent" id="search-results" aria-labelledby="search-results-heading">
			<header class="guide-section__heading">
				<span class="section-index">01 // SEARCH</span>
				<h2 id="search-results-heading">{data.query}</h2>
				<If cond={data.has_query == false}><p>Search the versioned corpus by canonical term, incoming alias, or task question.</p></If>
				<If cond={data.has_query}><p>Top answers are ranked by explicit match quality, then stable category/title/topic order.</p></If>
			</header>
			<If cond={data.has_query && data.has_results}>
				<div class="help-result-list">
					<Each of={data.search_results} as="result">
						<a class="help-result" href={"/help/" + result.id} data-gosx-link>
							<span class="section-index">{result.category}</span>
							<strong>{result.title}</strong>
							<If cond={result.has_runtime_note}><span class="mono">{result.runtime_note}</span></If>
							<span>{result.summary}</span>
							<span class="help-result__arrow" aria-hidden="true">→</span>
						</a>
					</Each>
				</div>
			</If>
			<If cond={data.has_query && data.has_results == false}>
				<div class="guide-callout guide-callout--alert" role="status"><strong>No matching topic.</strong> Try a canonical noun such as team, Big Board, waiver, trade, Pick'em, or live data. The underlying league state is unchanged.</div>
			</If>
			<If cond={data.has_query == false}>
				<div class="guide-callout" role="note"><strong>Recovery rule:</strong> if the answer depends on a date, rule, role, deadline, score, or data source, the current league page wins. This corpus explains the decision and links back to the owning route.</div>
			</If>
		</section>

		<section class="guide-section" id="topic-corpus" aria-labelledby="topic-corpus-heading">
			<header class="guide-section__heading"><span class="section-index">02 // VERSIONED TOPICS</span><h2 id="topic-corpus-heading">One corpus. Stable routes.</h2><p>Browse by task instead of searching.</p></header>
			<Each of={data.categories} as="category">
				<section class="help-category" id={category.id} aria-labelledby={category.id + "-heading"}>
					<h3 id={category.id + "-heading"}>{category.title}</h3>
					<div class="guide-card-grid guide-card-grid--three">
						<Each of={category.topics} as="topic">
							<article class="guide-card help-topic-card">
								<span class="section-index">/{topic.id}</span>
								<TextBlock as="h4" font="600 16px Plus Jakarta Sans" lineHeight={22} maxLines={2} overflow="ellipsis" text={topic.title} />
								<TextBlock as="p" font="400 15px Plus Jakarta Sans" lineHeight={22} maxLines={3} overflow="ellipsis" text={topic.summary} />
								<a class="guide-card__link" href={"/help/" + topic.id} data-gosx-link>Open topic →</a>
							</article>
						</Each>
					</div>
				</section>
			</Each>
		</section>

		<section class="guide-section guide-section--accent" id="checklists" aria-labelledby="checklists-heading">
			<header class="guide-section__heading"><span class="section-index">03 // ROLE + PHASE</span><h2 id="checklists-heading">The checklist follows the person.</h2><p>Base admitted-member guidance composes with primary, co-manager, seatless, and commissioner-overlay predicates. Unsupported capabilities are not dead obligations.</p></header>
			<div class="guide-card-grid guide-card-grid--two">
				<article class="guide-card"><span class="section-index">PRIMARY MANAGER</span><h3>Before the next action</h3><ol class="guide-checklist"><Each of={data.checklist} as="item"><If cond={item.applicable}><li><strong>{item.title}</strong><span>{item.detail} <a href={item.action_route} data-gosx-link>{item.action_label}</a></span></li></If></Each></ol></article>
				<article class="guide-card"><span class="section-index">COMMISSIONER OVERLAY</span><h3>Operate the owning room</h3><ol class="guide-checklist"><Each of={data.commissioner_checklist} as="item"><If cond={item.applicable}><li><strong>{item.title}</strong><span>{item.detail} <a href={item.action_route} data-gosx-link>{item.action_label}</a></span></li></If></Each></ol></article>
			</div>
			<div class="guide-callout" role="note"><strong>Co-manager truth:</strong> a current Big Board is keyed per account. This help center does not promise shared visibility, merged ordering, attribution, detach migration, or shared autopick before the owner decision and implementation gate.</div>
		</section>

		<section class="guide-section" id="migration" aria-labelledby="migration-heading">
			<header class="guide-section__heading"><span class="section-index">04 // CONCEPT TRANSITION</span><h2 id="migration-heading">Bring the questions, verify the rules.</h2><p>There is no automatic account, roster, history, or password migration. Map concepts, then confirm the new runtime.</p></header>
			<div class="help-mapping-table" role="table" aria-label="Platform migration concept mappings">
				<div class="help-mapping-row help-mapping-row--head" role="row"><strong role="columnheader">Gridiron concept</strong><strong role="columnheader">Incoming alias</strong><strong role="columnheader">Material difference + next action</strong></div>
				<Each of={data.migration} as="mapping"><div class="help-mapping-row" role="row"><TextBlock as="strong" role="cell" font="600 16px Plus Jakarta Sans" lineHeight={22} maxLines={2} overflow="ellipsis" text={mapping.canonical} /><TextBlock as="span" role="cell" font="400 15px Plus Jakarta Sans" lineHeight={22} maxLines={3} overflow="ellipsis" text={mapping.incoming_aliases} /><TextBlock as="span" role="cell" font="400 15px Plus Jakarta Sans" lineHeight={22} maxLines={3} overflow="ellipsis">{mapping.difference} <b>Next:</b> {mapping.next_action}</TextBlock></div></Each>
			</div>
			<p class="scoring-note">Privacy, consequence, and runtime-source details are available on <a href="/help/concept-transition" data-gosx-link>the full concept-transition topic</a>.</p>
		</section>

		<section class="guide-section guide-section--accent" id="glossary" aria-labelledby="glossary-heading">
			<header class="guide-section__heading"><span class="section-index">05 // CANONICAL VOCABULARY</span><h2 id="glossary-heading">Say the thing that owns the rule.</h2><p>Aliases are searchable; canonical terms carry the product meaning.</p></header>
			<div class="help-glossary-grid"><Each of={data.glossary} as="entry"><article class="help-glossary-entry"><h3><TextBlock as="dfn" font="600 16px Plus Jakarta Sans" lineHeight={22} maxLines={2} overflow="ellipsis" text={entry.term} /></h3><TextBlock as="p" font="400 15px Plus Jakarta Sans" lineHeight={22} maxLines={4} overflow="ellipsis" text={entry.definition} /><If cond={entry.aliases}><small>Also searched as: {entry.aliases}</small></If><a href={"/help/" + entry.topic_id} data-gosx-link>Topic →</a></article></Each></div>
		</section>

		<section class="guide-section" id="recovery" aria-labelledby="recovery-heading">
			<header class="guide-section__heading"><span class="section-index">06 // STATE RECOVERY</span><h2 id="recovery-heading">Every state explains the way back.</h2><p>Loading, empty, no-results, pending, saved, locked, disabled, stale, degraded, offline, unavailable, failed, permission-denied, and not-applicable each have a reason, impact, remaining capability, preserved context, next action, retry rule, and topic link.</p></header>
			<div class="guide-card-grid guide-card-grid--three">
				<article class="guide-card"><span class="section-index">STALE / DEGRADED</span><h3>Keep the last good read</h3><p>Show source and last-success age. Preserve unaffected content. Unknown is not zero and a cached snapshot is not live.</p><a href="/help/data-state-and-freshness" data-gosx-link>Data-state topic →</a></article>
				<article class="guide-card"><span class="section-index">LOCKED / DISABLED</span><h3>Name the boundary</h3><p>Explain the role, prerequisite, kickoff, review, or final-state boundary and link to the adjacent valid action.</p><a href="/help/lineups-locks-matchups-and-scoring" data-gosx-link>Workflow topic →</a></article>
				<article class="guide-card"><span class="section-index">FAILED / DENIED</span><h3>Do not replay blindly</h3><p>Say whether any effect occurred, preserve safe return context, and retry from freshly rendered state or escalate to the owner.</p><a href="/help/commissioner-operations" data-gosx-link>Recovery topic →</a></article>
			</div>
		</section>

		<footer class="guide-next"><div><span class="section-index">CORPUS RECEIPT</span><h2>Verified vocabulary, live rules.</h2><p>Corpus {data.corpus_version} · source <span title={data.source_sha}>{data.source_sha_short}</span>. {data.runtime.runtime_note}</p></div><div class="guide-actions"><a href="/guide" data-gosx-link class="button button--primary">Open manager guide →</a><a href="/scoring" data-gosx-link class="button button--ghost">Open live rules →</a></div></footer>
		<a class="access-link back-to-top-link" href="#main-content">↑ Back to top</a>
	</main>
}
