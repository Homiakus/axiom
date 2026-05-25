package main

import (
	"fmt"
	"html/template"
	"net/url"
	"strings"
)

const defaultAssumptions = `System.ready=true
System.mode=auto
Safety.estop=false
Safety.locked=false
Water.levelStatus=normal
Uart.ready=true
Uart.connected=true
Uart.circuitOpen=false
Command.busy=false`

var pageTmpl *template.Template

const pageHTML = `<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="color-scheme" content="dark">
<title>Axiom Rule Studio</title>
<style>
:root{
  --bg:#070b12;--bg2:#0b111b;--panel:#101827;--panel2:#0d1421;--panel3:#121d2d;
  --text:#edf4ff;--muted:#94a3b8;--soft:#cbd5e1;--line:#243247;--line2:#334155;
  --accent:#38bdf8;--accent2:#818cf8;--good:#34d399;--bad:#fb7185;--warn:#fbbf24;--violet:#c4b5fd;
  --code:#060a11;--shadow:0 16px 48px rgba(0,0,0,.35);--radius:16px;--tap:44px;
}
*{box-sizing:border-box}html{background:var(--bg);scroll-behavior:smooth}body{margin:0;min-height:100vh;background:radial-gradient(circle at top left,#132036 0,#070b12 44%,#05070b 100%);color:var(--text);font:14px/1.48 system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Arial,sans-serif}a{color:var(--accent);text-decoration:none}a:hover{text-decoration:underline}code,pre,textarea{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,"Liberation Mono",monospace}.app{min-height:100vh;display:flex;flex-direction:column}.topbar{position:sticky;top:0;z-index:50;display:flex;align-items:center;justify-content:space-between;gap:14px;padding:12px 16px;border-bottom:1px solid var(--line);background:rgba(7,11,18,.9);backdrop-filter:blur(12px)}.brand{display:flex;align-items:center;gap:12px;min-width:0}.logo{width:38px;height:38px;border-radius:12px;background:linear-gradient(135deg,var(--accent),var(--accent2));box-shadow:0 0 28px rgba(56,189,248,.28)}.brand h1{margin:0;font-size:17px;line-height:1.1}.brand .meta{font-size:12px;color:var(--muted);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;max-width:52vw}.top-actions{display:flex;gap:8px;align-items:center;flex-wrap:wrap;justify-content:flex-end}.button,button{min-height:var(--tap);display:inline-flex;align-items:center;justify-content:center;gap:7px;background:#162236;color:var(--text);border:1px solid var(--line2);border-radius:12px;padding:9px 12px;cursor:pointer;font-weight:650}.button:hover,button:hover{border-color:var(--accent);background:#1b2a40;text-decoration:none}.button.primary,button.primary{background:linear-gradient(135deg,#0ea5e9,#6366f1);border:0}.button.ghost{background:transparent}.toolbar{display:grid;grid-template-columns:1fr auto;gap:10px;padding:12px 16px;border-bottom:1px solid var(--line);background:rgba(13,20,33,.72)}.toolbar form{display:flex;gap:8px;align-items:center;flex-wrap:wrap}.toolbar .downloads{display:flex;gap:8px;justify-content:flex-end;flex-wrap:wrap}input[type=text],input[type=search]{min-height:var(--tap);background:var(--code);color:var(--text);border:1px solid var(--line2);border-radius:12px;padding:9px 12px;min-width:280px;outline:none}input[type=text]:focus,input[type=search]:focus,textarea:focus{border-color:var(--accent);box-shadow:0 0 0 3px rgba(56,189,248,.16)}input[type=file]{color:var(--muted);max-width:220px}.msg{margin:12px 16px 0;padding:10px 12px;border:1px solid rgba(56,189,248,.35);background:rgba(14,165,233,.1);color:#bae6fd;border-radius:14px}.mobile-tabs{display:none;position:sticky;top:63px;z-index:40;padding:8px 10px;gap:8px;overflow:auto;border-bottom:1px solid var(--line);background:rgba(7,11,18,.94);backdrop-filter:blur(10px)}.mobile-tabs a{flex:0 0 auto;color:var(--soft);padding:9px 12px;border:1px solid var(--line2);border-radius:999px;background:#111a2a}.layout{display:grid;grid-template-columns:310px minmax(420px,1fr) 430px;gap:0;min-height:calc(100vh - 126px)}.col{min-width:0;padding:14px;border-right:1px solid var(--line);overflow:auto}.right{border-right:0}.panel{background:rgba(16,24,39,.94);border:1px solid var(--line);border-radius:var(--radius);margin-bottom:14px;overflow:hidden;box-shadow:var(--shadow)}.panel h2{font-size:13px;letter-spacing:.02em;text-transform:uppercase;color:#dbeafe;margin:0;padding:11px 13px;border-bottom:1px solid var(--line);background:linear-gradient(180deg,#142033,#101827)}.panel h3{font-size:13px;margin:14px 0 7px;color:#e0e7ff}.panel h3:first-child{margin-top:0}.body{padding:13px}.muted{color:var(--muted)}.small{font-size:12px}.stats{display:grid;grid-template-columns:repeat(2,1fr);gap:8px}.stat{background:#0b1220;border:1px solid var(--line);border-radius:13px;padding:10px}.stat b{display:block;font-size:20px}.stat span{color:var(--muted);font-size:12px}.nav-section{margin:0 0 10px}.nav-section summary{cursor:pointer;color:#dbeafe;font-weight:750;padding:8px;border-radius:10px}.nav-section summary:hover{background:#152238}.nav-item{display:flex;align-items:center;justify-content:space-between;gap:8px;min-height:38px;padding:8px 9px;border:1px solid transparent;border-radius:11px;margin:4px 0;color:var(--soft)}.nav-item:hover{background:#132034;text-decoration:none}.nav-item.active{background:linear-gradient(135deg,rgba(56,189,248,.16),rgba(129,140,248,.13));border-color:rgba(56,189,248,.36);color:white}.kind{color:var(--muted);font-size:10px;text-transform:uppercase;letter-spacing:.05em;white-space:nowrap}.pill{display:inline-flex;align-items:center;gap:4px;padding:4px 9px;border-radius:999px;background:#1f2937;margin:2px;color:#dbeafe;border:1px solid rgba(148,163,184,.16);font-size:12px}.pill.fn{background:rgba(99,102,241,.18);color:#ddd6fe}.pill.write{background:rgba(16,185,129,.12);color:#bbf7d0}.pill.read{background:rgba(14,165,233,.12);color:#bae6fd}.pill.warn{background:rgba(251,191,36,.12);color:#fde68a}.rule-title,.action-title{font-size:24px;line-height:1.1;margin:5px 0 8px}.action-title{color:var(--violet)}.grid2{display:grid;grid-template-columns:1fr 1fr;gap:14px}.grid3{display:grid;grid-template-columns:repeat(3,1fr);gap:10px}.list{margin:0;padding-left:18px}.list li{margin:5px 0}.code{white-space:pre-wrap;background:var(--code);border:1px solid var(--line);border-radius:13px;padding:11px;overflow:auto;font-size:12px}.source-editor{min-height:58vh}.smallarea{min-height:190px}textarea{width:100%;background:var(--code);color:#e2e8f0;border:1px solid var(--line2);border-radius:13px;padding:12px;font-size:12px;line-height:1.45;resize:vertical}.diag{margin:0;padding-left:18px}.diag li{margin-bottom:9px}.graph{display:grid;grid-template-columns:1fr;gap:8px}.flow-row{display:grid;grid-template-columns:1fr auto 1fr;gap:8px;align-items:center}.node{padding:11px;border:1px solid var(--line);border-radius:13px;background:#0b1220;white-space:pre-wrap;min-height:46px}.arrow{color:var(--muted);text-align:center}.status{font-weight:800}.pass,.runnable{color:var(--good)}.fail,.blocked{color:var(--bad)}.unknown{color:var(--warn)}.expand{color:var(--violet)}.step{border:1px solid var(--line);border-radius:14px;padding:11px;margin:9px 0;background:#0d1625;white-space:normal}.step-head{display:flex;justify-content:space-between;gap:8px;align-items:flex-start}.phase{color:var(--muted);font-size:12px}.kv{display:grid;grid-template-columns:150px 1fr;gap:8px;padding:7px 0;border-bottom:1px dashed rgba(148,163,184,.22)}.kv:last-child{border-bottom:0}.actioncard{border:1px solid var(--line);border-radius:16px;background:linear-gradient(180deg,#101827,#0b1220);padding:14px}.sim-help{display:grid;grid-template-columns:1fr;gap:8px}.chips{display:flex;flex-wrap:wrap;gap:5px}.footer{color:var(--muted);font-size:12px;padding:18px;text-align:center;border-top:1px solid var(--line);background:#060a11}details.final summary{cursor:pointer;color:#dbeafe;font-weight:750;padding:10px 0}.mobile-only{display:none}
@media(max-width:1280px){.layout{grid-template-columns:280px minmax(360px,1fr)}.right{grid-column:1/-1;border-top:1px solid var(--line);border-right:0}.toolbar{grid-template-columns:1fr}.toolbar .downloads{justify-content:flex-start}}
@media(max-width:820px){:root{--radius:14px}.topbar{align-items:flex-start;padding:11px 12px}.brand h1{font-size:16px}.brand .meta{max-width:70vw}.top-actions{display:none}.toolbar{padding:10px 12px}.toolbar form{width:100%}.toolbar input[type=text]{min-width:0;width:100%}.toolbar .downloads{display:grid;grid-template-columns:1fr 1fr;width:100%}.toolbar .downloads .button{width:100%}.mobile-tabs{display:flex}.layout{display:block;min-height:auto}.col{border-right:0;border-bottom:1px solid var(--line);padding:10px}.side,.right,.main{scroll-margin-top:112px}.panel{margin-bottom:10px;border-radius:14px}.body{padding:11px}.stats{grid-template-columns:repeat(3,1fr)}.grid2,.grid3{grid-template-columns:1fr}.rule-title,.action-title{font-size:21px}.source-editor{min-height:380px}.flow-row{grid-template-columns:1fr}.flow-row .arrow{transform:rotate(90deg);height:10px}.kv{grid-template-columns:1fr}.step-head{display:block}.mobile-only{display:initial}.desktop-only{display:none}input[type=file]{max-width:100%;width:100%}}
@media(max-width:430px){body{font-size:13px}.stats{grid-template-columns:repeat(2,1fr)}.toolbar .downloads{grid-template-columns:1fr}.button,button,input[type=text]{min-height:42px}.panel h2{font-size:12px}.nav-item{min-height:42px}.brand .meta{font-size:11px}.logo{width:34px;height:34px}}
@media(print){.topbar,.toolbar,.mobile-tabs,.side,.source-panel,.footer{display:none}.layout{display:block}.col{border:0}.panel{box-shadow:none;break-inside:avoid}}
.graph-panel{display:grid;gap:12px}.graph-controls{display:flex;gap:8px;flex-wrap:wrap;align-items:center}.graph-controls a{min-height:36px;display:inline-flex;align-items:center;padding:7px 10px;border:1px solid var(--line2);border-radius:10px;background:#0b1220;color:var(--soft);font-size:12px}.graph-controls a.active{border-color:var(--accent);color:white;background:rgba(56,189,248,.14)}.graph-stage{height:560px;min-height:420px;border:1px solid var(--line);border-radius:14px;background:#070b12;overflow:hidden;position:relative}.crfg-svg{width:100%;height:100%;display:block;touch-action:none}.graph-edge{fill:none;stroke:#334155;stroke-width:1.8;opacity:.72}.graph-edge.active{stroke:var(--accent);stroke-width:2.4;opacity:1}.graph-edge.write{stroke:#34d399}.graph-edge.protects{stroke:#fbbf24;stroke-dasharray:5 5}.graph-node rect{width:152px;height:58px;rx:8;fill:#101827;stroke:#334155;stroke-width:1.4}.graph-node.event rect{fill:#0b2531;stroke:#38bdf8}.graph-node.condition rect{fill:#182033;stroke:#818cf8}.graph-node.rule rect{fill:#12231c;stroke:#34d399}.graph-node.action rect{fill:#211b35;stroke:#c4b5fd}.graph-node.state rect{fill:#10202a;stroke:#22c55e}.graph-node.safety rect{fill:#2a210d;stroke:#fbbf24}.graph-node.selected rect,.graph-node.focused rect{stroke:white;stroke-width:2.6}.graph-node text{fill:#edf4ff;font-size:12px;pointer-events:none}.graph-node .node-kind{fill:#94a3b8;font-size:10px;text-transform:uppercase}.graph-node.runnable rect{filter:drop-shadow(0 0 8px rgba(52,211,153,.38))}.graph-node.blocked rect{stroke:#fb7185}.graph-node.unknown rect{stroke:#fbbf24}.graph-node.written rect{filter:drop-shadow(0 0 8px rgba(34,197,94,.36))}.graph-node.scheduled rect{filter:drop-shadow(0 0 8px rgba(196,181,253,.34))}.graph-node.dim{opacity:.35}.graph-tip{position:absolute;left:12px;bottom:10px;color:var(--muted);font-size:12px;background:rgba(6,10,17,.82);border:1px solid var(--line);border-radius:10px;padding:6px 8px}.graph-mobile{display:none}.timeline-item{display:grid;grid-template-columns:82px 1fr auto;gap:8px;align-items:center;padding:9px 0;border-bottom:1px dashed rgba(148,163,184,.2)}.timeline-item:last-child{border-bottom:0}.state-table{display:grid;gap:8px}.state-row{display:grid;grid-template-columns:1.2fr 1fr 1fr 1fr;gap:8px;padding:8px;border:1px solid var(--line);border-radius:10px;background:#0b1220}.diag-link{color:var(--soft)}.diag-link:hover{color:white}.mockarea{min-height:120px}
@media(max-width:820px){.graph-stage{display:none}.graph-mobile{display:block}.graph-controls a{flex:1 1 auto;justify-content:center}.timeline-item{grid-template-columns:1fr}.state-row{grid-template-columns:1fr}.mockarea{min-height:150px}}
</style>
</head>
<body><div class="app">
<header class="topbar">
  <div class="brand"><div class="logo" aria-hidden="true"></div><div><h1>Axiom Rule Studio</h1><div class="meta">{{.Model.SystemName}} · {{.Model.Path}}</div></div></div>
  <nav class="top-actions"><a class="button ghost" href="/stubs">Go stubs</a><a class="button ghost" href="/report">Report</a><a class="button ghost" href="/download-source">Source</a><a class="button primary" href="/zip">App zip</a></nav>
</header>
<section class="toolbar" aria-label="Project tools">
  <form action="/load" method="post"><input type="text" name="path" placeholder="Path to .axm" value="{{.Model.Path}}"><button>Load file</button></form>
  <div class="downloads"><form action="/upload" method="post" enctype="multipart/form-data"><input type="file" name="file" accept=".axm,.txt"><button>Upload</button></form><a class="button" href="/stubs">Go stubs</a><a class="button" href="/report">Markdown report</a><a class="button" href="/download-source">Download source</a></div>
</section>
<nav class="mobile-tabs"><a href="#project">Project</a><a href="#workspace">Rules</a><a href="#graph">Graph</a><a href="#simulation">Simulation</a><a href="#source">Source</a></nav>
{{if .Msg}}<div class="msg">{{.Msg}}</div>{{end}}
<div class="layout">
<aside id="project" class="col side">
  <div class="panel"><h2>Project health</h2><div class="body"><div class="stats"><div class="stat"><b>{{.Model.Format}}</b><span>Format</span></div><div class="stat"><b>{{if .Model.CompileOK}}OK{{else}}FAIL{{end}}</b><span>Compiler</span></div><div class="stat"><b>{{len .Model.Rules}}</b><span>Rules</span></div><div class="stat"><b>{{len .Model.Actions}}</b><span>Actions</span></div><div class="stat"><b>{{len .Model.States}}</b><span>States</span></div><div class="stat"><b>{{len .Model.Events}}</b><span>Events</span></div><div class="stat"><b>{{len .Model.Conditions}}</b><span>Conditions</span></div><div class="stat"><b>{{len .Model.Always}}</b><span>Always</span></div></div></div></div>
  <div class="panel"><h2>Action cards</h2><div class="body">{{range $name,$a := .Model.Actions}}<a class="nav-item {{if and $.HasAction (eq $.Action.Name $a.Name)}}active{{end}}" href="/?action={{$a.Name}}#workspace"><span>{{$a.Name}}</span><span class="kind">{{if $a.Declared}}function{{else}}inferred{{end}}</span></a>{{else}}<div class="muted">No actions detected.</div>{{end}}</div></div>
  <div class="panel"><h2>Scenarios / blocks</h2><div class="body">{{range .Model.Sections}}<details class="nav-section" open><summary>{{.Name}}</summary>{{range .Blocks}}<a class="nav-item {{if eq $.Selected.ID .ID}}active{{end}}" href="/?id={{.ID}}#workspace"><span>{{.Name}}</span><span class="kind">{{.Kind}}</span></a>{{end}}</details>{{end}}</div></div>
  <div class="panel"><h2>Production diagnostics</h2><div class="body"><ul class="diag">{{range .Model.CompilerDiagnostics}}<li><a class="diag-link" href="{{diagnosticURL .}}"><b>{{.Code}}</b>{{if .Line}} line {{.Line}}{{end}}{{if .Entity}} · {{.Entity}}{{end}}</a><br>{{.Message}}{{if .Hint}}<br><span class="muted">{{.Hint}}</span>{{end}}</li>{{end}}{{range .Model.Diagnostics}}<li><a class="diag-link" href="#source">{{.}}</a></li>{{else}}{{if not $.Model.CompilerDiagnostics}}<li class="muted">No diagnostics.</li>{{end}}{{end}}</ul></div></div>
</aside>
<main id="workspace" class="col main">
  {{if .HasAction}}<div class="panel"><h2>Action Card</h2><div class="body">{{template "actioncard" .Action}}</div></div>{{end}}
  <div class="panel"><h2>Selected block</h2><div class="body"><div class="muted small">{{.Selected.Kind}} · lines {{.Selected.StartLine}}–{{.Selected.EndLine}}</div><h1 class="rule-title">{{.Selected.Name}}</h1>{{if .HasRule}}{{template "rulecard" .}}{{else}}<pre class="code">{{.Selected.Source}}</pre>{{end}}</div></div>
  <div id="graph" class="panel"><h2>CRFG graph</h2><div class="body">{{template "graphview" .}}</div></div>
  <div id="state-inspector" class="panel"><h2>State inspector</h2><div class="body">{{template "stateinspector" .Graph}}</div></div>
  <div id="source" class="panel source-panel"><h2>Source editor</h2><div class="body"><form action="/update" method="post"><input type="hidden" name="selected" value="{{.Selected.ID}}"><textarea class="source-editor" name="source" spellcheck="false">{{.Model.Source}}</textarea><p><button class="primary">Parse from editor</button></p></form><form action="/save" method="post"><input type="hidden" name="source" value="{{.Model.Source}}"><input type="text" name="path" value="{{.Model.Path}}"><button>Save current source to path</button></form></div></div>
  <div class="panel"><h2>Normalized Axiom v0</h2><div class="body">{{if .Model.NormalizedSource}}<pre class="code">{{.Model.NormalizedSource}}</pre>{{else}}<span class="muted">Normalized view is unavailable until the compiler accepts the source.</span>{{end}}</div></div>
</main>
<section id="simulation" class="col right">
  <div class="panel"><h2>System simulation</h2><div class="body"><form method="get" action="/#simulation"><input type="hidden" name="id" value="{{.Selected.ID}}"><input type="hidden" name="graph" value="{{.Graph.Filter}}"><input type="text" name="event" placeholder="Event name, e.g. PHMeasurementDue" value="{{.EventName}}"><p class="muted small">Initial state / assumptions. One assignment per line.</p><textarea class="smallarea" name="assumptions" spellcheck="false">{{.Assumptions}}</textarea><p class="muted small">Mock action outputs as JSON. Used only for result.* writes.</p><textarea class="mockarea" name="mockOutputs" spellcheck="false" placeholder='{"MeasurePH":{"value":7.4,"status":"ok"}}'>{{.MockOutputs}}</textarea><p><button class="primary">Run simulation</button></p></form><div class="sim-help"><div class="chips"><span class="pill">RUNNABLE</span><span class="pill warn">UNKNOWN</span><span class="pill">BLOCKED</span></div><div class="muted small">The simulator is conservative: simple boolean and comparison expressions are evaluated; complex expressions stay UNKNOWN instead of producing false confidence.</div></div>{{template "simreport" .}}</div></div>
  <div class="panel"><h2>Explain selected rule</h2><div class="body">{{if .HasRule}}{{template "explain" .}}{{else}}<span class="muted">Select a rule.</span>{{end}}</div></div>
  <div class="panel"><h2>Code binding</h2><div class="body">{{if .HasRule}}{{range .Rule.Functions}}<a class="pill fn" href="/?action={{.}}#workspace">{{.}}()</a>{{else}}<span class="muted">No external function.</span>{{end}}{{else if .HasAction}}<div class="muted small">Required Go method:</div><pre class="code">func (a Actions) {{.Action.Name}}(ctx context.Context, input {{.Action.Name}}Input) ({{.Action.Name}}Output, error)</pre>{{else}}<span class="muted">Select a rule or action.</span>{{end}}<p><a href="/stubs">Download generated Go stubs</a></p></div></div>
</section>
</div><div class="footer">Production-oriented local Studio · CRFG graph · dark theme · mobile-first layout · pure Go server · no frontend framework.</div>
</div><script>
(function(){
  const svg = document.querySelector('.crfg-svg');
  if(!svg) return;
  let vb = {x:0,y:0,w:Number(svg.dataset.width)||980,h:Number(svg.dataset.height)||520};
  let dragging = false, last = null;
  function apply(){ svg.setAttribute('viewBox', [vb.x,vb.y,vb.w,vb.h].join(' ')); }
  apply();
  svg.addEventListener('wheel', function(e){
    e.preventDefault();
    const scale = e.deltaY > 0 ? 1.12 : 0.88;
    const mx = vb.x + vb.w / 2, my = vb.y + vb.h / 2;
    vb.w *= scale; vb.h *= scale; vb.x = mx - vb.w / 2; vb.y = my - vb.h / 2; apply();
  }, {passive:false});
  svg.addEventListener('pointerdown', function(e){ dragging = true; last = {x:e.clientX,y:e.clientY}; svg.setPointerCapture(e.pointerId); });
  svg.addEventListener('pointermove', function(e){
    if(!dragging || !last) return;
    vb.x -= (e.clientX-last.x) * vb.w / svg.clientWidth;
    vb.y -= (e.clientY-last.y) * vb.h / svg.clientHeight;
    last = {x:e.clientX,y:e.clientY}; apply();
  });
  svg.addEventListener('pointerup', function(){ dragging = false; last = null; });
  document.querySelectorAll('.graph-node').forEach(function(node){
    node.addEventListener('mouseenter', function(){
      const id = node.dataset.node;
      document.querySelectorAll('.graph-node,.graph-edge').forEach(function(el){ el.classList.add('dim'); });
      node.classList.remove('dim');
      document.querySelectorAll('[data-from="'+id+'"],[data-to="'+id+'"]').forEach(function(edge){
        edge.classList.remove('dim');
        const other = edge.dataset.from === id ? edge.dataset.to : edge.dataset.from;
        const n = document.querySelector('[data-node="'+other+'"]');
        if(n) n.classList.remove('dim');
      });
    });
    node.addEventListener('mouseleave', function(){
      document.querySelectorAll('.dim').forEach(function(el){ el.classList.remove('dim'); });
    });
  });
})();
</script></body></html>
{{define "actioncard"}}<div class="actioncard"><h1 class="action-title">{{.Name}}()</h1><div class="muted small">{{if .Declared}}Declared in source{{else}}Inferred from do-blocks{{end}}</div><div class="grid2"><div><h3>Called by rules</h3>{{range .CalledBy}}<span class="pill">{{.}}</span>{{else}}<span class="muted">No callers detected.</span>{{end}}<h3>Call forms</h3><ul class="list">{{range .CallForms}}<li><code>{{.}}</code></li>{{else}}<li class="muted">No calls.</li>{{end}}</ul><h3>Inputs</h3>{{range .Inputs}}<span class="pill read">{{.}}</span>{{else}}<span class="muted">No inputs detected.</span>{{end}}<h3>Implementation</h3><span class="pill {{if .Declared}}write{{else}}warn{{end}}">{{if .Declared}}declared in DSL{{else}}inferred, declaration missing{{end}}</span></div><div><h3>Outputs used</h3>{{range .Outputs}}<span class="pill fn">result.{{.}}</span>{{else}}<span class="muted">No result fields detected.</span>{{end}}<h3>Writes driven by this action</h3>{{range .Writes}}<span class="pill write">{{.}}</span>{{else}}<span class="muted">No writes detected.</span>{{end}}<h3>Profile / idempotency / safety</h3>{{range .SafetyHints}}<div class="pill warn">{{.}}</div>{{else}}<span class="muted">No explicit profile or safety hint detected.</span>{{end}}</div></div>{{if .Declared}}<h3>Source</h3><pre class="code">{{.Block.Source}}</pre>{{end}}</div>{{end}}
{{define "graphview"}}<div class="graph-panel"><div class="graph-controls"><a class="{{if eq .Graph.Filter "all"}}active{{end}}" href="{{graphFilterURL . "all"}}#graph">All</a><a class="{{if eq .Graph.Filter "selected"}}active{{end}}" href="{{graphFilterURL . "selected"}}#graph">Selected</a><a class="{{if eq .Graph.Filter "safety"}}active{{end}}" href="{{graphFilterURL . "safety"}}#graph">Safety</a><a class="{{if eq .Graph.Filter "runnable"}}active{{end}}" href="{{graphFilterURL . "runnable"}}#graph">Runnable path</a><a class="{{if eq .Graph.Filter "writes"}}active{{end}}" href="{{graphFilterURL . "writes"}}#graph">Writes</a></div><div class="graph-stage"><svg class="crfg-svg" data-width="{{.Graph.Width}}" data-height="{{.Graph.Height}}" role="img" aria-label="Axiom context reactive graph"><defs><marker id="arrowhead" markerWidth="10" markerHeight="7" refX="9" refY="3.5" orient="auto"><polygon points="0 0, 10 3.5, 0 7" fill="#64748b"></polygon></marker></defs><g class="edges">{{range .Graph.Edges}}<path class="graph-edge {{.Kind}} {{if .Active}}active{{end}}" data-from="{{.From}}" data-to="{{.To}}" d="{{edgePath $.Graph .}}" marker-end="url(#arrowhead)"><title>{{.Label}}</title></path>{{end}}</g><g class="nodes">{{range .Graph.Nodes}}<a href="{{.URL}}"><g class="graph-node {{.Kind}} {{statusClass .Status}} {{if .Selected}}selected{{end}} {{if .Focused}}focused{{end}}" data-node="{{.ID}}" transform="translate({{.X}},{{.Y}})"><rect x="-76" y="-29"></rect><text x="-64" y="-6">{{short .Label 22}}</text><text class="node-kind" x="-64" y="14">{{.Kind}}{{if .Status}} · {{.Status}}{{end}}</text><title>{{.Kind}}: {{.Label}}{{if .Detail}} · {{.Detail}}{{end}}</title></g></a>{{end}}</g></svg><div class="graph-tip">Wheel zoom, drag pan, hover to highlight neighbors.</div></div><div class="graph-mobile">{{range .Graph.Timeline}}<a class="timeline-item" href="{{.URL}}"><span class="kind">{{.Kind}}</span><span>{{.Label}}{{if .Detail}}<br><span class="muted small">{{.Detail}}</span>{{end}}</span>{{if .Status}}<span class="status {{statusClass .Status}}">{{.Status}}</span>{{end}}</a>{{else}}<span class="muted">No graph nodes detected.</span>{{end}}</div></div>{{end}}
{{define "stateinspector"}}<div class="state-table">{{range .StateFields}}<div class="state-row"><div><b>{{.Name}}</b></div><div><span class="muted small">Read by</span><br>{{range .ReadBy}}<span class="pill read">{{.}}</span>{{else}}<span class="muted">None</span>{{end}}</div><div><span class="muted small">Written by</span><br>{{range .WrittenBy}}<span class="pill write">{{.}}</span>{{else}}<span class="muted">None</span>{{end}}</div><div><span class="muted small">Protected by</span><br>{{range .ProtectedBy}}<span class="pill warn">{{.}}</span>{{else}}<span class="muted">None</span>{{end}}</div></div>{{else}}<span class="muted">No state dependencies detected.</span>{{end}}</div>{{end}}
{{define "rulecard"}}<div class="grid2"><div><h3>Starts</h3>{{if .Rule.OnEvent}}<p><span class="pill">{{.Rule.OnEvent}}</span></p>{{else}}<p class="muted">No explicit event.</p>{{end}}{{if .Rule.Every}}<p><span class="pill">every {{.Rule.Every}}</span></p>{{end}}<h3>Allowed if</h3><ul class="list">{{range .Rule.WhenLines}}<li>{{.}}</li>{{else}}<li class="muted">No conditions.</li>{{end}}</ul><h3>Do</h3><ul class="list">{{range .Rule.DoLines}}<li><code>{{.}}</code></li>{{else}}<li class="muted">No action.</li>{{end}}</ul></div><div><h3>Then</h3><ul class="list">{{range .Rule.ThenLines}}<li>{{.}}</li>{{else}}<li class="muted">No writes.</li>{{end}}</ul><h3>Reads</h3>{{range .Rule.Reads}}<span class="pill read">{{.}}</span>{{else}}<span class="muted">None detected.</span>{{end}}<h3>Writes</h3>{{range .Rule.Writes}}<span class="pill write">{{.}}</span>{{else}}<span class="muted">None detected.</span>{{end}}</div></div>{{end}}
{{define "explain"}}<p class="muted small">The evaluator expands named conditions and handles simple boolean/comparison expressions.</p><ul class="list">{{range .Rule.WhenLines}}{{ $all := evalCond $.Model . $.Assumptions }}{{range $all}}<li><span class="status {{statusClass .Status}}">{{.Status}}</span> — {{.Condition}}<br><span class="muted">{{.Why}}</span></li>{{end}}{{else}}<li class="runnable">RUNNABLE BY TRIGGER: no explicit conditions.</li>{{end}}</ul>{{end}}
{{define "simreport"}}{{if .EventName}}{{if .SimReport.Steps}}<h3>Timeline</h3>{{range .SimReport.Steps}}<div class="step"><div class="step-head"><b>#{{.Index}} {{.Rule}}</b><span class="phase">{{.Phase}}</span></div><div>Verdict: <span class="status {{statusClass .Verdict}}">{{.Verdict}}</span></div>{{if .Conditions}}<div class="muted small">Conditions:</div><ul class="list">{{range .Conditions}}<li><span class="status {{statusClass .Status}}">{{.Status}}</span> — {{.Condition}} <span class="muted">{{.Why}}</span></li>{{end}}</ul>{{end}}{{if .Actions}}<div class="muted small">Actions planned:</div>{{range .Actions}}<span class="pill fn">{{.}}</span>{{end}}{{end}}{{if .Writes}}<div class="muted small">State writes:</div>{{range .Writes}}<div class="kv"><span>{{.Target}}</span><b>{{.Value}}</b></div>{{end}}{{end}}{{if .Note}}<div class="muted small">{{.Note}}</div>{{end}}</div>{{end}}<details class="final"><summary>Final simulated state</summary>{{range .SimReport.FinalState}}<div class="kv"><span>{{.Key}}</span><span>{{.Value}}</span></div>{{end}}</details>{{else}}<div class="muted">No matching event-triggered rules found. Try exact event name from the Events list.</div>{{end}}{{end}}{{end}}
`

func init() {
	pageTmpl = template.Must(template.New("page").Funcs(template.FuncMap{
		"join":        strings.Join,
		"contains":    strings.Contains,
		"statusClass": func(s string) string { return strings.ToLower(s) },
		"short": func(s string, max int) string {
			if len(s) <= max {
				return s
			}
			if max <= 1 {
				return s[:max]
			}
			return s[:max-1] + "..."
		},
		"edgePath":       graphTemplateEdgePath,
		"graphFilterURL": graphFilterURL,
		"diagnosticURL":  diagnosticURL,
		"explainCond": func(cond, assumptions string) EvalResult {
			return explainCondition(cond, parseAssumptions(assumptions))
		},
		"evalCond": func(m ProjectModel, cond, assumptions string) []CondEval {
			return evalConditionExpanded(m, cond, parseAssumptions(assumptions), map[string]bool{})
		},
	}).Parse(pageHTML))
}

func graphTemplateEdgePath(g ProjectGraph, e GraphEdge) string {
	var from, to GraphNode
	for _, n := range g.Nodes {
		if n.ID == e.From {
			from = n
		}
		if n.ID == e.To {
			to = n
		}
	}
	if from.ID == "" || to.ID == "" {
		return ""
	}
	return fmt.Sprintf("M%d,%d C%d,%d %d,%d %d,%d", from.X+76, from.Y, from.X+150, from.Y, to.X-150, to.Y, to.X-76, to.Y)
}

func graphFilterURL(data PageData, filter string) string {
	q := url.Values{}
	if data.Selected.ID != "" {
		q.Set("id", data.Selected.ID)
	}
	if data.HasAction {
		q.Set("action", data.Action.Name)
	}
	if data.EventName != "" {
		q.Set("event", data.EventName)
	}
	if data.Assumptions != "" && data.Assumptions != defaultAssumptions {
		q.Set("assumptions", data.Assumptions)
	}
	if data.MockOutputs != "" {
		q.Set("mockOutputs", data.MockOutputs)
	}
	q.Set("graph", filter)
	if data.FocusNode != "" {
		q.Set("focus", data.FocusNode)
	}
	return "/?" + q.Encode()
}

func diagnosticURL(d CompilerDiagnostic) string {
	if d.Entity != "" {
		switch d.Kind {
		case "rule":
			return "/?id=rule:" + url.QueryEscape(d.Entity) + "#workspace"
		case "activity", "function":
			return "/?action=" + url.QueryEscape(d.Entity) + "#workspace"
		case "context", "state":
			return "#state-inspector"
		}
	}
	return "#source"
}
