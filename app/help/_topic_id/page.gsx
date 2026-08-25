package topic

func Page() Node {
	return <main class="page guide-page help-page help-topic-page" id="main-content">
		<header class="page-masthead guide-masthead help-masthead">
			<div>
				<span class="signal-label"><span class="signal-mark" aria-hidden="true"></span>HELP TOPIC // {data.topic.category}</span>
				<p class="page-kicker">/{data.topic.id} · corpus {data.corpus_version} · verified {data.source_sha}</p>
				<h1>{data.topic.title}</h1>
				<p class="guide-lede">{data.topic.summary}</p>
				<nav class="guide-actions" aria-label="Topic actions">
					<a href={data.topic.action_route} data-gosx-link class="button button--primary">Open owning action →</a>
					<a href="/help" data-gosx-link class="button button--ghost">Back to help center</a>
				</nav>
			</div>
			<aside class="masthead-console guide-console" aria-label="Topic receipt">
				<div><span>Actor</span><strong>{data.topic.actor}</strong></div>
				<div><span>Supported</span><strong>{data.topic.supported}</strong></div>
				<div><span>Runtime source</span><strong>{data.topic.runtime_source}</strong></div>
				<div><span>Verified source</span><strong>{data.source_sha}</strong></div>
			</aside>
		</header>

		<If cond={data.has_state || data.has_field}>
			<section class="guide-section help-context-panel" id="context" aria-labelledby="context-heading">
				<header class="guide-section__heading"><span class="section-index">CONTEXTUAL HELP</span><h2 id="context-heading">Read this field or state before acting.</h2><p>This panel is derived from the same topic record and keeps mutable values with the owning runtime page.</p></header>
				<div class="guide-card-grid guide-card-grid--two">
					<If cond={data.has_state}>
						<article class="guide-card"><span class="section-index">STATE // {data.state_help.state}</span><h3>{data.state_help.why}</h3><p><strong>Impact:</strong> {data.state_help.impact}</p><p><strong>Still available:</strong> {data.state_help.remaining}</p><p><strong>Keep:</strong> {data.state_help.context}</p><p><strong>Next:</strong> {data.state_help.next_action}</p><p><strong>Retry:</strong> {data.state_help.retry}</p><a href={"/help/" + data.topic.id + "?field=state"} data-gosx-link class="guide-card__link">Explain the state field →</a></article>
					</If>
					<If cond={data.has_field}>
						<article class="guide-card"><span class="section-index">FIELD // {data.field_help.label}</span><h3>Runtime-owned field help</h3><p>{data.field_help.help}</p><p><strong>Source:</strong> {data.field_help.runtime_source}</p><a href={data.field_help.next_action} data-gosx-link class="guide-card__link">Open owning action →</a></article>
					</If>
				</div>
			</section>
		</If>

		<nav class="guide-toc help-toc" aria-label="Topic sections">
			<a href="#prerequisites">Prerequisites</a>
			<a href="#states">States + deadline</a>
			<a href="#steps">Steps</a>
			<a href="#consequence">Consequence</a>
			<a href="#recovery">Failure + recovery</a>
			<a href="#example">Example</a>
		</nav>

		<section class="guide-section guide-section--accent" id="prerequisites" aria-labelledby="prerequisites-heading">
			<header class="guide-section__heading"><span class="section-index">01 // ACTOR + PREDICATE</span><h2 id="prerequisites-heading">Who can use this answer.</h2></header>
			<div class="guide-card-grid guide-card-grid--two">
				<article class="guide-card"><span class="section-index">ACTOR</span><h3>{data.topic.actor}</h3><p>{data.topic.supported}</p></article>
				<article class="guide-card"><span class="section-index">PREREQUISITE</span><h3>Read the current gate</h3><p>{data.topic.prerequisites}</p></article>
			</div>
		</section>

		<section class="guide-section" id="states" aria-labelledby="states-heading">
			<header class="guide-section__heading"><span class="section-index">02 // STATE + TIME</span><h2 id="states-heading">The label carries the consequence.</h2><p>{data.topic.states}</p></header>
			<div class="guide-compare">
				<article class="guide-compare__panel guide-compare__panel--signal"><span class="section-index">DEADLINE</span><h3>Use the runtime clock</h3><p>{data.topic.deadline}</p></article>
				<article class="guide-compare__panel"><span class="section-index">PRIVACY</span><h3>Keep the right audience</h3><p>{data.topic.privacy}</p></article>
			</div>
		</section>

		<section class="guide-section guide-section--accent" id="steps" aria-labelledby="steps-heading">
			<header class="guide-section__heading"><span class="section-index">03 // WORKFLOW</span><h2 id="steps-heading">Take the next safe step.</h2></header>
			<ol class="guide-steps"><Each of={data.topic.steps} as="step"><li><span class="guide-step__mark mono">→</span><div><p>{step}</p></div></li></Each></ol>
		</section>

		<section class="guide-section" id="consequence" aria-labelledby="consequence-heading">
			<header class="guide-section__heading"><span class="section-index">04 // EFFECT</span><h2 id="consequence-heading">Know what changes.</h2></header>
			<div class="guide-card-grid guide-card-grid--three">
				<article class="guide-card"><span class="section-index">CONSEQUENCE</span><h3>What happens</h3><p>{data.topic.consequence}</p></article>
				<article class="guide-card"><span class="section-index">REVERSIBILITY</span><h3>What can be undone</h3><p>{data.topic.reversibility}</p></article>
				<article class="guide-card"><span class="section-index">RESULT</span><h3>What you should see</h3><p>{data.topic.result}</p></article>
			</div>
		</section>

		<section class="guide-section guide-section--accent" id="recovery" aria-labelledby="recovery-heading">
			<header class="guide-section__heading"><span class="section-index">05 // FAILURE + RECOVERY</span><h2 id="recovery-heading">Keep context. Do not replay blindly.</h2></header>
			<div class="guide-compare">
				<article class="guide-compare__panel"><span class="section-index">FAILURE</span><h3>{data.topic.failure}</h3></article>
				<article class="guide-compare__panel guide-compare__panel--signal"><span class="section-index">RECOVERY</span><h3>{data.topic.recovery}</h3></article>
			</div>
		</section>

		<section class="guide-section" id="example" aria-labelledby="example-heading">
			<header class="guide-section__heading"><span class="section-index">06 // SOURCE + EXAMPLE</span><h2 id="example-heading">One concrete read.</h2></header>
			<div class="guide-callout" role="note"><strong>Example:</strong> {data.topic.example}</div>
			<p class="scoring-note"><strong>Runtime source:</strong> {data.topic.runtime_source}</p>
			<p class="scoring-note"><strong>Corpus receipt:</strong> introduced {data.topic.introduced_version}; last verified {data.topic.last_verified_sha}.</p>
		</section>

		<footer class="guide-next"><div><span class="section-index">NEXT TRANSMISSION</span><h2>Use the owning route.</h2><p>When mutable rules, dates, capabilities, or freshness disagree with a topic, the current runtime page wins.</p></div><div class="guide-actions"><a href={data.topic.action_route} data-gosx-link class="button button--primary">Open {data.topic.action_route} →</a><a href="/help" data-gosx-link class="button button--ghost">Search another topic</a></div></footer>
	</main>
}
