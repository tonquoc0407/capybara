import { useEffect, useRef, useState } from 'react';
import SpotlightCard from './components/SpotlightCard.jsx';
import GlareHover from './components/GlareHover.jsx';
import GradientText from './components/GradientText.jsx';
import StarBorder from './components/StarBorder.jsx';

const repo = 'https://github.com/tonquoc0407/capybara';
const installCommands = {
  brew: 'brew install tonquoc0407/tap/capybara',
  source: 'go install github.com/tonquoc0407/capybara/cmd/capybara@main'
};
const connections = [
  { id: 'otlp', label: 'OTLP', summary: 'Already emitting OTLP over HTTP? Point the exporter at the local receiver.', code: 'export OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318', href: '#otlp' },
  { id: 'python', label: 'Python SDK', summary: 'Install capybara-sdk, then add these lines to your Python app.', code: 'import capybara\ncapybara.init()', href: '#from-your-own-agent' },
  { id: 'claude', label: 'Claude Code', summary: 'Read an existing Claude Code session from local logs.', code: 'capybara watch claude', href: '#a-session-file' },
  { id: 'json', label: 'HTTP events', summary: 'Set the endpoint for the included examples or for your own event sender.', code: 'export CAPYBARA_EVENTS_URL=http://127.0.0.1:4318/api/v1/events', href: '#from-your-own-agent' }
];
const totalSeconds = 9;
const marqueeItems = ['OTLP INPUT', 'LOCAL HTTP API', 'FULL TRACE TREE', 'LINKED LOGS', 'COST PROVENANCE', 'REGRESSION CHECKS'];

// A labelled, synthetic example of the failure documented in the project tour.
const trace = [
  {
    id: 'run', at: 0, depth: 0, kind: 'RUN', name: 'price answer after tool failure', duration: '8.4s', status: 'running',
    detail: 'The agent needs a current quote before it can answer. This replay follows its recorded steps.',
    input: 'What is the current value of my 120 ACME shares?', output: 'The answer should depend on a fresh price lookup.'
  },
  {
    id: 'plan', at: 1.2, depth: 1, kind: 'MODEL', name: 'plan quote', duration: '420ms', status: 'ok',
    detail: 'The model plans to fetch holdings and the current share price.',
    input: 'Question: current value of 120 ACME shares', output: 'Read portfolio holdings, then request the current ACME price.'
  },
  {
    id: 'context', at: 2.5, depth: 1, kind: 'RETRIEVAL', name: 'portfolio context', duration: '230ms', status: 'ok',
    detail: 'The portfolio lookup returns the share count, but no live market price.',
    input: 'Find ACME holdings', output: 'Portfolio holds 120 ACME shares.'
  },
  {
    id: 'price', at: 3.9, depth: 1, kind: 'TOOL', name: 'lookup_price', duration: '560ms', status: 'error',
    detail: 'The price service fails. The run has no verified current price at this point.',
    input: '{ "symbol": "ACME" }', output: 'Error: pricing service timed out.'
  },
  {
    id: 'cache', at: 5.2, depth: 1, kind: 'TOOL', name: 'read_cached_quote', duration: '240ms', status: 'ok',
    detail: 'A cached quote exists, but it is stale and cannot establish the current price.',
    input: '{ "symbol": "ACME" }', output: 'Cached quote: $42.00, last updated yesterday.'
  },
  {
    id: 'answer', at: 6.7, depth: 1, kind: 'MODEL', name: 'answer', duration: '700ms', status: 'finding',
    detail: 'The model states a current value after the live lookup failed. Capybara marks this turn for review.',
    input: 'Current quote request failed; cached quote is from yesterday.',
    output: 'The current value of your 120 ACME shares is $5,040.'
  },
  {
    id: 'finding', at: 8, depth: 1, kind: 'FINDING', name: 'improvised', duration: '—', status: 'finding',
    detail: 'A failed tool call was followed by a confident answer. This finding is a clue to review, not proof of fabrication.',
    input: 'lookup_price: error', output: 'Review the answer or add a regression fixture for this case.'
  }
];

function CopyButton({ value, children, className = '' }) {
  const [message, setMessage] = useState('');

  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setMessage('Copied');
    } catch {
      setMessage('Select the command to copy');
    }
    window.setTimeout(() => setMessage(''), 2500);
  }

  return <button type="button" className={className} onClick={copy} aria-label={`Copy ${children}`}>
    <span>{message || children}</span><span aria-hidden="true">↗</span>
  </button>;
}

function TraceReplay() {
  const [time, setTime] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [speed, setSpeed] = useState(1);
  const [selectedId, setSelectedId] = useState(null);
  const frame = useRef(0);
  const previous = useRef(0);

  useEffect(() => {
    if (!playing) return undefined;
    previous.current = 0;
    const tick = now => {
      if (previous.current) setTime(value => Math.min(totalSeconds, value + ((now - previous.current) / 1000) * speed));
      previous.current = now;
      frame.current = requestAnimationFrame(tick);
    };
    frame.current = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame.current);
  }, [playing, speed]);

  useEffect(() => {
    if (time >= totalSeconds && playing) setPlaying(false);
  }, [time, playing]);

  const visible = trace.filter(step => step.at <= time);
  const active = visible.at(-1) || trace[0];
  const selected = trace.find(step => step.id === selectedId) || active;
  const findingVisible = time >= trace.at(-1).at;

  function toggle() {
    if (time >= totalSeconds) setTime(0);
    setSelectedId(null);
    setPlaying(!playing);
  }

  function inspect(step) {
    setPlaying(false);
    setTime(Math.max(time, step.at));
    setSelectedId(step.id);
  }

  function advance() {
    const next = trace.find(step => step.at > time + 0.05);
    if (!next) return;
    setPlaying(false);
    setTime(next.at);
    setSelectedId(next.id);
  }

  return <div className="replay-shell" id="replay">
    <div className="replay-topline"><span>RUN 01 <span className="topline-divider">/</span> LOCAL DEMO</span><span className="replay-live">SYNTHETIC TRACE</span></div>
    <div className="replay-toolbar">
      <div>
        <p className="eyebrow">INTERACTIVE REPLAY</p>
        <h3>See why the answer was flagged.</h3>
      </div>
      <span className="run-count">01 RUN <span>·</span> 07 EVENTS</span>
    </div>
    <div className="replay-controls">
      <button type="button" className="play-button" onClick={toggle} aria-label={playing ? 'Pause replay' : 'Play replay'}>{playing ? 'Ⅱ Pause' : time >= totalSeconds ? '↺ Replay' : '▶ Play'}</button>
      <button type="button" className="step-button" onClick={advance} disabled={time >= trace.at(-1).at} aria-label="Next event">Next step →</button>
      <label className="sr-only" htmlFor="trace-progress">Replay position</label>
      <input id="trace-progress" className="progress" type="range" min="0" max={totalSeconds} step="0.01" value={time} style={{ '--progress': `${time / totalSeconds * 100}%` }} onChange={event => { setPlaying(false); setSelectedId(null); setTime(Number(event.target.value)); }} />
      <span className="clock">{time.toFixed(1)} / {totalSeconds}.0s</span>
      <button type="button" className="speed-button" onClick={() => setSpeed(speed === 1 ? 1.5 : speed === 1.5 ? 2 : 1)} aria-label={`Playback speed ${speed}x. Change speed`}>{speed}×</button>
    </div>
    <div className="replay-body">
      <div className="trace-pane">
        <div className="pane-title"><span>TRACE TREE</span><span>select a step ↓</span></div>
        <div className="trace-list">
          {trace.map(step => {
            const reached = step.at <= time;
            return <button key={step.id} type="button" onClick={() => inspect(step)} className={`trace-row ${selected.id === step.id ? 'selected' : ''} ${!reached ? 'unreached' : ''} status-${step.status}`} aria-current={selected.id === step.id ? 'step' : undefined}>
              <span className="tree-mark" aria-hidden="true">{step.depth ? '└─' : '▸'}</span>
              <span className="trace-symbol" aria-hidden="true">{!reached ? '○' : step.status === 'error' ? '×' : step.status === 'finding' ? '!' : '•'}</span>
              <span className="trace-name">{step.name}</span>
              <span className="trace-duration">{step.duration}</span>
            </button>;
          })}
        </div>
        <div className={`finding-strip ${findingVisible ? 'shown' : ''}`} aria-live="polite">
          <span className="finding-icon">!</span>
          <span>{findingVisible ? 'improvised · answer after tool failure' : 'Finding appears after the final turn'}</span>
        </div>
      </div>
      <div className="inspector-pane">
        <div className="pane-title"><span>INSPECTOR</span><span>{selected.kind}</span></div>
        <div className="inspector-content" key={selected.id} aria-live="polite">
          <div className="inspector-heading"><span className={`status-pill status-${selected.status}`}>{selected.status === 'finding' ? 'FINDING' : selected.status.toUpperCase()}</span><span className="inspector-duration">{selected.duration}</span></div>
          <h4>{selected.name}</h4>
          <p>{selected.detail}</p>
          <div className="payload"><span>INPUT</span><pre>{selected.input}</pre></div>
          <div className="payload"><span>OUTPUT</span><pre>{selected.output}</pre></div>
        </div>
      </div>
    </div>
    <div className="replay-caption"><span>Browser-only synthetic replay. Findings are review clues.</span><span>click a row · scrub · change speed</span></div>
  </div>;
}

function InstallTerminal() {
  const [method, setMethod] = useState('brew');
  const command = installCommands[method];

  return <div className="install-terminal">
    <div className="terminal-heading"><span>QUICK START</span><span>LOCAL INSTALL ↘</span></div>
    <div className="install-tabs" role="group" aria-label="Installation method">
      <button type="button" className={method === 'brew' ? 'active' : ''} onClick={() => setMethod('brew')} aria-pressed={method === 'brew'}>Homebrew</button>
      <button type="button" className={method === 'source' ? 'active' : ''} onClick={() => setMethod('source')} aria-pressed={method === 'source'}>Current source</button>
    </div>
    <div className="command-line"><span>$</span><code>{command}</code><CopyButton value={command} className="copy-command">Copy</CopyButton></div>
    <div className="command-line"><span>$</span><code>capybara sample && capybara</code><CopyButton value="capybara sample && capybara" className="copy-command">Copy</CopyButton></div>
    <p>{method === 'brew' ? 'Homebrew installs the latest published release. It may lag the features on main.' : 'Current source requires Go 1.26.10. Go may download the matching toolchain.'} <a href={`${repo}/releases`} target="_blank" rel="noreferrer">See releases ↗</a></p>
  </div>;
}

function ConnectPanel() {
  const [method, setMethod] = useState(connections[0]);
  return <div className="connect-panel">
    <div className="connect-tabs" role="group" aria-label="Connection methods">{connections.map(item => <button key={item.id} type="button" className={method.id === item.id ? 'active' : ''} aria-pressed={method.id === item.id} onClick={() => setMethod(item)}>{item.label}</button>)}</div>
    <div className="connect-content"><div><span className="connect-index">SOURCE / {method.id.toUpperCase()}</span><h3>{method.label}</h3><p>{method.summary}</p><a href={`${repo}${method.href}`} target="_blank" rel="noreferrer">Full setup in the README ↗</a></div><div className="connect-code"><span>CONNECT TO LOCALHOST</span><pre>{method.code}</pre><CopyButton value={method.code} className="copy-connect">Copy snippet</CopyButton></div></div>
  </div>;
}

function Marquee() {
  return <section className="marquee" aria-label={`Capabilities: ${marqueeItems.join(', ')}`}>
    <div className="marquee-track" aria-hidden="true">{[0, 1].map(copy => <div className="marquee-copy" key={copy}>{marqueeItems.map(item => <span key={item}>{item}<b>✳</b></span>)}</div>)}</div>
  </section>;
}

function App() {
  useEffect(() => {
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches || !('IntersectionObserver' in window)) return undefined;
    const elements = [...document.querySelectorAll('[data-reveal]')];
    const observer = new IntersectionObserver(entries => {
      for (const entry of entries) {
        if (entry.isIntersecting) {
          entry.target.classList.add('is-visible');
          observer.unobserve(entry.target);
        }
      }
    }, { threshold: 0.08, rootMargin: '0px 0px -25px 0px' });
    document.documentElement.classList.add('reveal-enabled');
    elements.forEach(element => observer.observe(element));
    return () => {
      observer.disconnect();
      document.documentElement.classList.remove('reveal-enabled');
    };
  }, []);

  const features = [
    { number: '01 / CAPTURE', title: 'Get a trace in.', body: 'OTLP, HTTP events, Python SDK, or Claude Code logs.', tag: 'OTLP · HTTP · SDK · FILE' },
    { number: '02 / INVESTIGATE', title: 'Follow the failure.', body: 'Inspect full spans, linked logs, findings, and costs.', tag: 'TUI · LOCAL WEB' },
    { number: '03 / VERIFY', title: 'Keep the fix.', body: 'Diff runs and gate regressions in CI.', tag: 'DIFF · REPLAY · CI' }
  ];

  return <>
    <a className="skip-link" href="#main">Skip to content</a>
    <header className="site-header">
      <a className="brand" href="#top" aria-label="Capybara home"><img src="/assets/logo.png" width="32" height="32" alt="" /><span>capybara<span className="brand-dot">.</span></span></a>
      <nav aria-label="Main navigation"><a href="#replay">Live demo</a><a href="#how-it-works">How it works</a><a href="#interfaces">Interfaces</a></nav>
      <a className="header-github" href={repo} target="_blank" rel="noreferrer">GitHub <span aria-hidden="true">↗</span></a>
    </header>
    <main id="main">
      <section className="hero" id="top">
        <div className="hero-copy">
          <div className="eyebrow hero-eyebrow"><span className="pulse" /> LOCAL-FIRST AGENT TRACING <span className="eyebrow-line" /></div>
          <h1>Find the step that <GradientText colors={['#ffb271', '#f68477', '#ffe0af']} animationSpeed={7} glow={0.12} className="hero-gradient">broke</GradientText> your agent run<span className="period">.</span></h1>
          <p className="hero-lede">Inspect agent traces, tool failures, linked logs, and costs in a local TUI.</p>
          <div className="hero-actions"><a className="button-primary" href="#replay">Try the trace replay <span aria-hidden="true">↘</span></a><StarBorder as="a" className="button-secondary star-cta" href="#install" color="#ffb271" trailColor="#f68477" backgroundColor="#261b15" textColor="#f6eee5" borderColor="rgba(255, 178, 113, .26)" duration={6} stars={2} glow={0.65} sparkle={false} radius={8}>Get started <span aria-hidden="true">→</span></StarBorder></div>
        </div>
        <div className="hero-visual" aria-label="Capybara terminal interface screenshot">
          <GlareHover className="hero-glare" width="100%" glareColor="#ffb271" glareOpacity={0.22} glareSize={42} variant="sheen" mode="hover" transitionDuration={950} borderRadius="4px"><div className="hero-shot"><img src="/assets/pupu-workspace.png" width="1400" height="780" alt="Capybara TUI with a failed lookup_price span and an improvised answer finding" /></div></GlareHover>
        </div>
      </section>

      <Marquee />

      <section className="section replay-section" aria-labelledby="demo-title">
        <div className="section-intro" data-reveal><div><p className="eyebrow">01 / EXPLORE THE FAILURE</p><h2 id="demo-title">Inspect a failed run here.</h2></div></div>
        <div data-reveal><TraceReplay /></div>
        <div className="real-demo" data-reveal><div><p className="eyebrow">ACTUAL TUI RECORDING</p><h3>See the trace arrive.</h3><a href={`${repo}/blob/main/demo/local-api.tape`} target="_blank" rel="noreferrer">Recording script ↗</a></div><video autoPlay muted loop playsInline preload="metadata" poster="/assets/pupu-workspace.png" aria-label="Capybara TUI receiving and inspecting a local HTTP trace"><source src="/assets/pupu-tour.mp4" type="video/mp4" />Your browser does not support video playback.</video></div>
      </section>

      <section className="section workflow-section" id="how-it-works" aria-labelledby="workflow-title">
        <div className="section-intro" data-reveal><div><p className="eyebrow">02 / FROM TRACE TO FIX</p><h2 id="workflow-title">From trace to regression check.</h2></div></div>
        <div className="feature-grid" data-reveal>{features.map(feature => <SpotlightCard key={feature.number} className="feature-card" spotlightColor="#ffad68" intensity={0.24} spotlightSize={300} softness={0.8} flare={false} style={{ '--spotlight-card-surface': '#211b17', '--spotlight-card-border': 'rgba(255, 183, 125, .14)', '--spotlight-card-shadow': 'none' }}><span className="feature-number">{feature.number}</span><div className="feature-arrow" aria-hidden="true">↗</div><h3>{feature.title}</h3><p>{feature.body}</p><span className="feature-tag">{feature.tag}</span></SpotlightCard>)}</div>
      </section>

      <section className="section connect-section" aria-labelledby="connect-title"><div className="section-intro" data-reveal><div><p className="eyebrow">03 / CONNECT YOUR APP</p><h2 id="connect-title">Use the traces you already have.</h2></div></div><div data-reveal><ConnectPanel /></div></section>

      <section className="section interfaces-section" id="interfaces" aria-labelledby="interfaces-title">
        <div className="section-intro" data-reveal><div><p className="eyebrow">04 / INVESTIGATE</p><h2 id="interfaces-title">TUI for the run. Web for the wider view.</h2></div></div>
        <div className="interface-grid" data-reveal>
          <div className="interface-card interface-tui"><img loading="lazy" src="/assets/pupu-workspace.png" width="1400" height="780" alt="Capybara terminal workspace showing runs, trace tree, and inspector" /><div className="interface-caption"><span>TUI / INVESTIGATE</span><a href={`${repo}#reading-a-trace`} target="_blank" rel="noreferrer">Explore keys ↗</a></div></div>
          <div className="interface-card interface-web"><img loading="lazy" src="/assets/web-overview.png" width="1440" height="900" alt="Capybara web overview showing runs, errors, latency, cost, and incident panels" /><div className="interface-caption"><span>WEB / REVIEW</span><a href={`${repo}#product-tour`} target="_blank" rel="noreferrer">View tour ↗</a></div></div>
        </div>
        <div className="privacy-line" data-reveal><span className="privacy-icon" aria-hidden="true">⌁</span><p>Traces stay in local SQLite. Sharing through the hub is optional.</p></div>
      </section>

      <section className="section install-section" id="install" aria-labelledby="install-title">
        <div className="install-copy" data-reveal><p className="eyebrow">05 / RUN IT LOCALLY</p><h2 id="install-title">Get your first trace locally.</h2><p>Install, open the sample, then press <kbd>i</kbd> to connect your app.</p><a href={`${repo}/blob/main/docs/beta-test.md`} target="_blank" rel="noreferrer">Beta test guide <span aria-hidden="true">↗</span></a></div>
        <div data-reveal><InstallTerminal /></div>
      </section>
    </main>
    <footer className="site-footer"><div><a className="brand" href="#top"><img src="/assets/logo.png" width="28" height="28" alt="" /><span>capybara<span className="brand-dot">.</span></span></a><p>A local-first trace debugger for AI agents.</p></div><div className="footer-links"><a href={repo} target="_blank" rel="noreferrer">Source ↗</a><a href={`${repo}/issues/new/choose`} target="_blank" rel="noreferrer">Feedback ↗</a><a href={`${repo}/blob/main/LICENSE`} target="_blank" rel="noreferrer">MIT License ↗</a><a href="/assets/react-bits-license.txt">React Bits license ↗</a></div></footer>
  </>;
}

export default App;
