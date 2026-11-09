import { useEffect, useRef, useState } from 'react';
import GradientText from './components/GradientText.jsx';

const repo = 'https://github.com/tonquoc0407/capybara';
const installCommands = {
  brew: 'brew install tonquoc0407/tap/capybara',
  source: 'go install github.com/tonquoc0407/capybara/cmd/capybara@main'
};
const connections = [
  { id: 'otlp', label: 'OTLP', summary: 'Point your existing OTLP HTTP exporter at the local receiver, then make one traced request.', code: 'export OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318', result: 'The new run appears in Connect; press o to inspect it.', href: '#otlp' },
  { id: 'python', label: 'Python SDK', summary: 'Install capybara-sdk and trace a function in your app.', code: 'pip install capybara-sdk', result: 'Call your traced function; the run appears in Connect.', href: '#from-your-own-agent' },
  { id: 'claude', label: 'Claude Code', summary: 'The TUI automatically tails local Claude Code logs. To watch without the TUI:', code: 'capybara watch claude', result: 'Start a Claude Code turn, then open its run in the TUI.', href: '#a-session-file' },
  { id: 'json', label: 'HTTP events', summary: 'From a source checkout, send a complete run to the local HTTP receiver.', code: 'python examples/local_events.py', result: 'The example prints a run ID; press o in Connect to inspect it.', href: '#from-your-own-agent' }
];
const supportedInputs = [
  { name: 'OTLP', setup: 'Send HTTP/protobuf or gRPC to localhost.', note: 'Known GenAI attributes are mapped; other sources may need config.', href: '#otlp' },
  { name: 'HTTP events', setup: 'POST span events to the local receiver.', note: 'Loopback clients only; no API key.', href: 'docs/local-events.md' },
  { name: 'Python SDK', setup: 'Install capybara-sdk from PyPI.', note: 'Call init() and trace a function.', href: '#from-your-own-agent' },
  { name: 'Node SDK', setup: 'Install from the repository source.', note: 'Not published on npm yet.', href: 'sdk-js/README.md' },
  { name: 'Claude Code', setup: 'Read local session logs.', note: 'No app instrumentation needed.', href: '#a-session-file' },
  { name: 'Files', setup: 'Import JSONL or agent-replay JSON.', note: 'OTLP LogRecords are not imported as traces.', href: '#a-file-you-have' }
];
const totalSeconds = 9;
const marqueeItems = ['OTLP INPUT', 'LOCAL HTTP API', 'FULL TRACE TREE', 'LINKED LOGS', 'COST PROVENANCE', 'REGRESSION CHECKS'];
const interfaceViews = [
  { id: 'tui', label: 'Terminal', title: 'Trace tree and inspector', image: '/assets/pupu-workspace.png', source: `${repo}/blob/main/demo/pupu-workspace.png`, alt: 'Capybara TUI with runs, trace tree, findings, and span inspector' },
  { id: 'overview', label: 'Web overview', title: 'Runs, errors, latency, and cost', image: '/assets/web-overview.png', source: `${repo}/blob/main/demo/web-overview.png`, alt: 'Capybara Web overview with run metrics and incidents' },
  { id: 'trace', label: 'Web trace', title: 'Execution and evidence on mobile', image: '/assets/web-trace-mobile.png', source: `${repo}/blob/main/demo/web-trace-mobile.png`, alt: 'Capybara Web trace with execution tree and evidence on a narrow screen' }
];

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
  const [method, setMethod] = useState('source');
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
  return <><div className="connect-journey" aria-label="First trace steps"><span><b>1</b> <a href="#install">Start Capybara</a></span><span><b>2</b> Send one traced run</span><span><b>3</b> Open it in Connect</span></div><div className="connect-panel">
    <div className="connect-tabs" role="group" aria-label="Connection methods">{connections.map(item => <button key={item.id} type="button" className={method.id === item.id ? 'active' : ''} aria-pressed={method.id === item.id} onClick={() => setMethod(item)}>{item.label}</button>)}</div>
    <div className="connect-content"><div><h3>{method.label}</h3><p>{method.summary}</p><a href={`${repo}${method.href}`} target="_blank" rel="noreferrer">Full setup ↗</a></div><div className="connect-code"><pre>{method.code}</pre><CopyButton value={method.code} className="copy-connect">Copy</CopyButton><p className="connect-result">{method.result}</p></div></div>
  </div></>;
}

function SupportedInputs() {
  return <div className="supported-inputs" id="sources"><h3>Supported inputs</h3><div className="source-list">{supportedInputs.map(input => <a className="source-row" key={input.name} href={input.href.startsWith('#') ? `${repo}${input.href}` : `${repo}/blob/main/${input.href}`} target="_blank" rel="noreferrer">
    <span className="source-heading"><strong>{input.name}</strong><span aria-hidden="true">↗</span></span><span className="source-setup">{input.setup}</span><span className="source-note">{input.note}</span>
  </a>)}</div></div>;
}

function PublicCase() {
  return <section className="section case-section" id="case" aria-labelledby="case-title">
    <div className="section-intro" data-reveal><h2 id="case-title">A public bug, traced to a status mismatch.</h2><p>OpenAI Agents SDK issue #4070, reproduced on v0.19.1 with a local model stub. No production traffic.</p></div>
    <div className="case-layout" data-reveal><div className="case-media"><video autoPlay muted loop playsInline preload="metadata" onLoadedMetadata={event => { event.currentTarget.currentTime = 6; }} onTimeUpdate={event => { if (event.currentTarget.currentTime >= 16.5) event.currentTarget.currentTime = 6; }} aria-label="Capybara imports the issue 4070 trace, compares runs and checks a regression fixture"><source src="/assets/agents-issue-4070.mp4" type="video/mp4" />Your browser does not support video playback.</video></div><div className="case-evidence">
      <div><span>01</span><p>The model span failed, but the regular agent span said <code>ok</code>.</p></div>
      <div><span>02</span><p>Comparing regular and streamed runs shows the first status difference.</p></div>
      <div><span>03</span><p>The exported golden check exits with a failure on that difference.</p></div>
      <div className="case-links"><a href="/assets/agents-issue-4070.mp4" target="_blank" rel="noreferrer">Open recording ↗</a><a href={`${repo}/tree/main/demo/external/agents_issue_4070`} target="_blank" rel="noreferrer">Trace and reproducer ↗</a></div>
    </div></div>
  </section>;
}

function InterfaceShowcase() {
  const [selectedId, setSelectedId] = useState('tui');
  const selected = interfaceViews.find(view => view.id === selectedId);

  return <div className="interface-showcase">
    <div className="showcase-tabs" role="group" aria-label="Choose interface preview">{interfaceViews.map(view => <button key={view.id} type="button" aria-pressed={selectedId === view.id} onClick={() => setSelectedId(view.id)}>{view.label}</button>)}</div>
    <div className={`showcase-stage showcase-${selected.id}`} key={selected.id} role="region" aria-label={`${selected.label} screenshot; scroll to see more`} tabIndex={0}><img src={selected.image} width={selected.id === 'tui' ? 1400 : selected.id === 'overview' ? 1280 : 390} height={selected.id === 'tui' ? 780 : selected.id === 'overview' ? 1275 : 1622} alt={selected.alt} /></div>
    <div className="showcase-caption"><span>{selected.title}</span><a href={selected.source} target="_blank" rel="noreferrer">Open full image ↗</a></div>
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

  return <>
    <a className="skip-link" href="#main">Skip to content</a>
    <header className="site-header">
      <a className="brand" href="#top" aria-label="Capybara home"><img src="/assets/logo.png" width="32" height="32" alt="" /><span>capybara<span className="brand-dot">.</span></span></a>
      <nav aria-label="Main navigation"><a href="#replay">Demo</a><a href="#case">Real case</a><a href="#sources">Inputs</a><a href="#beta">Beta test</a></nav>
      <a className="header-github" href={repo} target="_blank" rel="noreferrer">GitHub <span aria-hidden="true">↗</span></a>
    </header>
    <main id="main">
      <section className="hero" id="top">
        <div className="hero-copy">
          <div className="eyebrow hero-eyebrow"><span className="pulse" /> LOCAL-FIRST AGENT TRACING <span className="eyebrow-line" /></div>
          <h1>Find the step that <GradientText colors={['#ffb271', '#f68477', '#ffe0af']} animationSpeed={7} className="hero-gradient">broke</GradientText> your agent run<span className="period">.</span></h1>
          <p className="hero-lede">Inspect agent traces, tool failures, linked logs, and costs in a local TUI.</p>
          <div className="hero-actions"><a className="button-primary" href="#replay">Try the trace replay <span aria-hidden="true">↘</span></a><a className="button-secondary" href="#install">Get started <span aria-hidden="true">→</span></a></div>
        </div>
        <div className="hero-visual" aria-label="Capybara terminal interface screenshot">
          <div className="hero-shot"><img src="/assets/pupu-workspace.png" width="1400" height="780" alt="Capybara TUI with a failed lookup_price span and an improvised answer finding" /></div>
        </div>
      </section>

      <Marquee />

      <section className="section replay-section" aria-labelledby="demo-title">
        <div className="section-intro" data-reveal><h2 id="demo-title">Inspect a failed run here.</h2></div>
        <div data-reveal><TraceReplay /></div>
        <div className="recording" data-reveal><div className="recording-heading"><h3>See the trace arrive.</h3></div><div className="recording-media"><video autoPlay muted loop playsInline preload="metadata" poster="/assets/pupu-workspace.png" aria-label="Capybara TUI receiving and inspecting a local HTTP trace"><source src="/assets/pupu-tour.mp4" type="video/mp4" />Your browser does not support video playback.</video></div><div className="recording-caption"><span>TUI recording · local HTTP trace</span><a href="/assets/pupu-tour.mp4" target="_blank" rel="noreferrer">Open video ↗</a></div></div>
      </section>

      <PublicCase />

      <section className="section connect-section" aria-labelledby="connect-title"><div className="section-intro" data-reveal><h2 id="connect-title">Use the traces you already have.</h2></div><div data-reveal><ConnectPanel /></div><div data-reveal><SupportedInputs /></div></section>

      <section className="section interfaces-section" id="interfaces" aria-labelledby="interfaces-title">
        <div className="section-intro" data-reveal><h2 id="interfaces-title">Inspect in terminal or browser.</h2></div>
        <div data-reveal><InterfaceShowcase /></div>
        <p className="privacy-line" data-reveal>Traces stay in local SQLite. Sharing through the hub is optional.</p>
      </section>

      <section className="section install-section" id="install" aria-labelledby="install-title">
        <div className="install-copy" data-reveal><h2 id="install-title">Get your first trace locally.</h2><p>Install, open the labelled sample, then press <kbd>i</kbd> to connect your app.</p><a href={`${repo}/blob/main/docs/beta-test.md`} target="_blank" rel="noreferrer">First trace guide <span aria-hidden="true">↗</span></a></div>
        <div data-reveal><InstallTerminal /></div>
      </section>
      <section className="beta-section" id="beta" aria-labelledby="beta-title"><div data-reveal><h2 id="beta-title">Try it on one of your runs.</h2><p>Follow the first trace guide, then send us what worked or where you got stuck. This local setup stores traces in SQLite on your machine.</p></div><div className="beta-actions" data-reveal><a className="button-primary" href={`${repo}/blob/main/docs/beta-test.md`} target="_blank" rel="noreferrer">Start beta test <span aria-hidden="true">↗</span></a><a className="button-secondary" href={`${repo}/issues/new/choose`} target="_blank" rel="noreferrer">Report feedback <span aria-hidden="true">↗</span></a></div></section>
    </main>
    <footer className="site-footer"><div><a className="brand" href="#top"><img src="/assets/logo.png" width="28" height="28" alt="" /><span>capybara<span className="brand-dot">.</span></span></a><p>A local-first trace debugger for AI agents.</p></div><div className="footer-links"><a href={repo} target="_blank" rel="noreferrer">Source ↗</a><a href={`${repo}/issues/new/choose`} target="_blank" rel="noreferrer">Feedback ↗</a><a href={`${repo}/blob/main/LICENSE`} target="_blank" rel="noreferrer">MIT License ↗</a><a href="/assets/react-bits-license.txt">React Bits license ↗</a></div></footer>
  </>;
}

export default App;
