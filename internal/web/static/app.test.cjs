/* Node's built-in runner; no new runtime/test dependencies. The small DOM
   adapter rejects HTML sinks and exercises the real rendering functions. */
const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
class Node {
 constructor(tag='div'){this.tagName=tag;this.children=[];this.attributes={};this.value='';this.hidden=false;this.className='';this.dataset={};this.classList={add:()=>{},toggle:()=>{}};}
 set textContent(value){this.text=String(value);this.children=[];}
 get textContent(){return (this.text||'')+this.children.map(n=>n.textContent||'').join('');}
 set innerHTML(_){throw new Error('Untrusted HTML sink used');}
 set outerHTML(_){throw new Error('Untrusted HTML sink used');}
 append(...nodes){this.children.push(...nodes);}
 replaceChildren(...nodes){this.text='';this.children=nodes;if(this.tagName==='select')this.value='';}
 add(node){if(!this.children.length)this.value=node.value;this.append(node);}
 get options(){return this.children;}
 setAttribute(k,v){this.attributes[k]=v;}
 getAttribute(k){return this.attributes[k];}
 contains(){return false;}
}
function app(){
 const nodes=new Map(),tags=[];
 const context=vm.createContext({
  document:{createElement:tag=>{tags.push(tag);return new Node(tag);},getElementById:id=>{if(!nodes.has(id))nodes.set(id,new Node(id==='compare-session'?'select':'div'));return nodes.get(id);},querySelectorAll:()=>[]},
  window:{addEventListener:()=>{}},navigator:{platform:'Win32'},localStorage:{getItem:()=>null},history:{replaceState:()=>{}},location:{hash:'',pathname:'/',search:''},
  URLSearchParams,Option:class extends Node{constructor(text,value){super('option');this.textContent=text;this.value=value;}},setTimeout:()=>0,clearTimeout:()=>{},setInterval:()=>0,fetch:()=>new Promise(()=>{}),console
 });
 context.document.documentElement={dataset:{}};
 vm.runInContext(fs.readFileSync(__dirname+'/app.js','utf8'),context);
 return {context,nodes,tags};
}
test('malicious payloads and filenames stay literal text in real render functions',()=>{
 const {context,tags}=app();context.parent=new Node();context.attack='<img src=x onerror="globalThis.pwned=true"><script>globalThis.pwned=true</script>';
 vm.runInContext('rawDetails(parent,{seq:4,type:attack,ts:"2026-01-01T00:00:00Z",raw:attack});',context);
 assert.match(context.parent.textContent,/<script>/);
 assert.equal(tags.includes('script'),false);assert.equal(tags.includes('img'),false);
 assert.equal(context.pwned,undefined);
});
test('empty, failure and outcome states remain understandable text',()=>{
 const {context}=app();context.parent=new Node();vm.runInContext('empty(parent,"No calls","Discovery is not a tool call");',context);
 assert.match(context.parent.textContent,/Discovery is not a tool call/);
 assert.equal(vm.runInContext('badge("failed").textContent',context),'Tool error');
 assert.equal(vm.runInContext('badge("denied").textContent',context),'Denied');
 assert.equal(vm.runInContext('fmtDuration(null)',context),'Timing unavailable');
});
test('CLI guidance quotes untrusted Windows paths as literal PowerShell arguments',()=>{
 const {context}=app();context.filename="C:\\My Sessions\\x'$(touch) .jsonl";
 assert.equal(vm.runInContext('shellArg(filename)',context),"'C:\\My Sessions\\x''$(touch) .jsonl'");
});
test('explicit Save as refuses non-empty files and writes only the reviewed YAML',async()=>{
 const {context,nodes}=app();
 context.created=false;
 context.window.showSaveFilePicker=async()=>({getFile:async()=>({size:12}),createWritable:async()=>{context.created=true;}});
 vm.runInContext('yamlPreview="version: 1\\n";',context);
 await vm.runInContext('saveTest()',context);
 assert.equal(context.created,false);
 assert.match(nodes.get('test-result').textContent,/already contains data/);
 context.window.showSaveFilePicker=async()=>({getFile:async()=>({size:0}),createWritable:async()=>({write:async text=>{context.saved=text;},close:async()=>{context.closed=true;}})});
 await vm.runInContext('saveTest()',context);
 assert.equal(context.saved,'version: 1\n');assert.equal(context.closed,true);
});
test('changing the selected call invalidates an in-flight test preview',async()=>{
 const {context,nodes}=app();
 let release;
 context.testResponse=new Promise(resolve=>{release=resolve;});
 context.document.getElementById("assertion-kind").value="attempted";
 vm.runInContext('currentFile="a.jsonl";selectedCall={seq:3};detailRevision="snapshot";api=()=>testResponse;',context);
 const pending=vm.runInContext('generateTest(true)',context);
 vm.runInContext('selectedCall={seq:6};clearTest();',context);
 release({yaml:'stale preview',passed:true,scope:'test'});
 await pending;
 assert.equal(nodes.get('test-yaml').hidden,true);
 assert.equal(nodes.get('save-test').disabled,true);
});
test('comparison retains its pair across tabs and clears stale results on a new choice',async()=>{
 const {context,nodes}=app();
 vm.runInContext('currentFile="a.jsonl";api=async()=>({sessions:[{file:"b.jsonl",name:"B"},{file:"c.jsonl",name:"C"}]});',context);
 const select=nodes.get('compare-session');select.value='c.jsonl';
 await vm.runInContext('loadCompareChoices()',context);
 assert.equal(select.value,'c.jsonl');
 context.document.getElementById('compare-result').textContent='old result';select.value='b.jsonl';select.onchange();
 assert.equal(nodes.get('compare-result').textContent,'');assert.equal(nodes.get('run-compare').disabled,false);
});
test('selected calls are visible, reorderable, and removed without HTML interpretation',()=>{
 const {context,nodes}=app();
 vm.runInContext('selectedEvents=[{seq:3,tool:"<script>danger</script>",result_seq:4},{seq:6,tool:"second",result_seq:7}];updateSelectedEvents();moveSelected(1,-1);',context);
 assert.match(nodes.get('selected-events').textContent,/<script>danger<\/script>/);
 assert.equal(vm.runInContext('selectedEvents[0].tool',context),'second');
 assert.equal(nodes.get('save-test').disabled,true);
});
test('recorded text extraction remains literal and tolerates non-JSON previews',()=>{
 const {context}=app();context.value='{"content":[{"text":"<img src=x onerror=alert(1)>"}]}';
 assert.equal(vm.runInContext('readableRecorded(value)',context),'<img src=x onerror=alert(1)>');
 context.value='truncated raw';assert.equal(vm.runInContext('readableRecorded(value)',context),'truncated raw');
});
test('policy decisions are stated in plain language, never as raw verdicts',()=>{
 const {context}=app();
 assert.equal(vm.runInContext('policyActionLabel("allow")',context),'Allow');
 assert.equal(vm.runInContext('policyActionLabel("deny")',context),'Block');
 assert.equal(vm.runInContext('policyActionLabel("confirm")',context),'Require approval');
 assert.match(vm.runInContext('policyActionMeaning("deny")',context),/not forwarded/);
 assert.equal(vm.runInContext('transitionText({changed:true,recorded_verdict:"allow",candidate_verdict:"deny"})',context),'Allow → Block');
 assert.equal(vm.runInContext('transitionText({changed:false,recorded_verdict:"allow",candidate_verdict:"allow"})',context),'No change');
});
test('simulation filters select exactly the requested decision changes',()=>{
 const {context}=app();
 const simulation={total:5,changed:3,same:2,newly_denied:1,newly_allowed:1,newly_confirm:1,calls:[
  {order:1,changed:false,candidate_verdict:'allow'},
  {order:2,changed:true,candidate_verdict:'deny'},
  {order:3,changed:true,candidate_verdict:'confirm'},
  {order:4,changed:true,candidate_verdict:'allow'},
  {order:5,changed:false,candidate_verdict:'deny'}]};
 context.simulation=simulation;
 const orders=f=>vm.runInContext(`filterSimulated(simulation,"${f}").map(c=>c.order)`,context);
 assert.deepEqual(orders('changed'),[2,3,4]);
 assert.deepEqual(orders('denied'),[2]);
 assert.deepEqual(orders('allowed'),[4]);
 assert.deepEqual(orders('confirm'),[3]);
 assert.deepEqual(orders('all'),[1,2,3,4,5]);
 assert.deepEqual(orders('same'),[1,5]);
 assert.equal(JSON.stringify(vm.runInContext('simulationCounts(simulation)',context)),JSON.stringify({total:5,changed:3,same:2,denied:1,allowed:1,confirm:1}));
});
test('policy cards render hostile rule text literally and keep raw conditions',()=>{
 const {context,tags}=app();
 context.rule={position:1,id:'</script><img src=x onerror=globalThis.pwned=true>',action:'deny',broad:true,
  broad_why:['the pattern "*" matches every tool name'],tool_summary:'calls to every tool',
  scope_summary:'any task label',action_effect:'refused',summary:'Block every tool.',
  conditions:[{expr:'context.args.cmd like "%rm -rf /%"',simplified:false,problem:'This pattern contains % , which Cedar treats as a literal.'}],
  where_raw:['context.args.cmd like "%rm -rf /%"']};
 const card=vm.runInContext('ruleCard(rule)',context);
 assert.match(card.textContent,/<img src=x/);
 assert.match(card.textContent,/Never reached|Broad permission/);
 assert.match(card.textContent,/Cannot match as written/);
 assert.match(card.textContent,/not simplified/);
 assert.equal(tags.includes('img'),false);assert.equal(tags.includes('script'),false);
 assert.equal(context.pwned,undefined);
});
test('an unsupported condition is never given an invented summary',()=>{
 const {context}=app();
 context.cond={expr:'context.args.a like "*x*y*"',simplified:false};
 assert.match(vm.runInContext('conditionItem(cond).textContent',context),/not simplified/);
 context.cond={expr:'context.args.path like "/tmp/*"',simplified:true,summary:'argument "path" starts with "/tmp/"',note:'A call without this key does not match.'};
 const text=vm.runInContext('conditionItem(cond).textContent',context);
 assert.match(text,/starts with/);assert.match(text,/does not match/);
});
test('the candidate save refuses a non-empty file and writes only reviewed text',async()=>{
 const {context,nodes}=app();
 vm.runInContext('studioState.candidate={valid:true,candidate:"version: 1\\ndefault: deny\\n"};',context);
 context.window.showSaveFilePicker=async()=>({getFile:async()=>({size:9}),createWritable:async()=>{context.created=true;}});
 await vm.runInContext('saveCandidate()',context);
 assert.equal(context.created,undefined);
 assert.match(nodes.get('candidate-status').textContent,/already contains data/);
 context.window.showSaveFilePicker=async()=>({getFile:async()=>({size:0}),createWritable:async()=>({write:async text=>{context.saved=text;},close:async()=>{context.closed=true;}})});
 await vm.runInContext('saveCandidate()',context);
 assert.equal(context.saved,'version: 1\ndefault: deny\n');assert.equal(context.closed,true);
 assert.match(nodes.get('candidate-status').textContent,/not active/);
});
test('an invalid candidate can never be saved',async()=>{
 const {context}=app();
 vm.runInContext('studioState.candidate={valid:false,candidate:"broken"};',context);
 context.window.showSaveFilePicker=async()=>{context.opened=true;};
 await vm.runInContext('saveCandidate()',context);
 assert.equal(context.opened,undefined);
});
test('builder helpers trim lists and convert local time to RFC 3339',()=>{
 const {context}=app();
 assert.equal(vm.runInContext('commaList(" a, b ,,c ").join("|")',context),'a|b|c');
 assert.equal(vm.runInContext('expiresRFC3339("")',context),'');
 assert.match(vm.runInContext('expiresRFC3339("2026-12-31T23:59")',context),/^2026-12-3\dT\d\d:59:00Z$/);
 assert.throws(()=>vm.runInContext('expiresRFC3339("not-a-date")',context),/cannot be read/);
});
test('session limits are optional, whole numbers, and omitted when blank',()=>{
 const {context}=app();
 const field=id=>context.document.getElementById(id);
 const set=(calls,perMinute,bytes)=>{field('builder-max-calls').value=calls;field('builder-max-per-minute').value=perMinute;field('builder-max-bytes').value=bytes;};
 set('','','');
 assert.equal(vm.runInContext('readBuilderLimits()',context),null);
 set('300','','');
 assert.equal(JSON.stringify(vm.runInContext('readBuilderLimits()',context)),JSON.stringify({max_calls:300,max_per_minute:0,max_response_bytes:0}));
 set('1.5','','');
 assert.throws(()=>vm.runInContext('readBuilderLimits()',context),/whole number/);
 set('-2','','');
 assert.throws(()=>vm.runInContext('readBuilderLimits()',context),/whole number/);
});
test('the limits fieldset is offered only for a new candidate',()=>{
 const {context}=app();
 const field=id=>context.document.getElementById(id);
 const base=field('builder-base'),fieldset=field('builder-limits-fieldset');
 base.value='new';
 vm.runInContext('syncLimitsAvailability()',context);
 assert.equal(fieldset.disabled,false);
 base.value='pack:starter';
 vm.runInContext('syncLimitsAvailability()',context);
 assert.equal(fieldset.disabled,true);
});
/* ------------------- M6: behaviour intelligence ------------------- */
// A minimal but realistic overview, shaped exactly like /api/behaviour returns.
function overview(){
  return {
    window:{from:"2026-02-01",to:"2026-02-28"},
    library:{files:8,analysed:8,matched:8,undated:0,cached:8,capped:false,truncated:false,skipped:[],budget_files:120,budget_bytes:50331648},
    counts:{sessions:8,calls:39,results:34,successes:26,failures:8,denies:4,deny_decisions:4,confirms:1,confirm_decisions:1,unanswered:5,discovery:8,events:135,repeat_findings:3,repeat_attention:2,signature_groups:1},
    tools:[{tool:"files__read",calls:5,results:5,failures:0,denies:0,sessions:3,error_rate:0}],
    denied:[{tool:"git__push",rule:"no-force-push",calls:4,sessions:1,last_ts:"2026-02-02T08:00:05Z"}],
    failures:[{
      id:"abc123",tool:"db__query",code:"-32000",label:"relation <text> does not exist",
      fragment:"does not exist",criteria:"same tool; same error code -32000",occurrences:5,sessions:2,
      first_ts:"2026-02-04T22:00:00Z",last_ts:"2026-02-27T14:00:00Z",files:["02-retry-loop.jsonl"],
      recent:[{file:"02-retry-loop.jsonl",seq:6,tool:"db__query",index:1}]
    }],
    repeats:[
      {kind:"possible_retry_loop",tool:"db__query",findings:1,occurrences:4,sessions:1,max_count:4,files:["02-retry-loop.jsonl"],sample:{file:"02-retry-loop.jsonl",seq:6,index:1}},
      {kind:"repeated_activity",tool:"jobs__status",findings:1,occurrences:6,sessions:1,max_count:6,files:["03-polling.jsonl"]},
      {kind:"insufficient_evidence",tool:"queue__pop",findings:1,occurrences:2,sessions:1,max_count:2,files:["05-alternating.jsonl"]}
    ],
    trend:[{file:"01-baseline.jsonl",name:"release 4.2",servers:["git"],calls:5,results:5,failures:0,denies:0,unanswered:0,repeat_findings:0,repeat_attention:0,error_rate:0,denial_rate:0,duration_ms:96000,complete:true,integrity:"verified",chain_ok:true}],
    recent:[],
    definitions:[{term:"Tool call",meaning:"One tools/call event."}],
    notes:["5 calls were never answered."],
    scope:"Derived only from recorded MCP events."
  };
}
test('a repeat loop is an interpretation and insufficient evidence is never called a finding',()=>{
  const {context,nodes}=app();
  context.ov=overview();
  vm.runInContext('renderBehaviourRepeats(ov)',context);
  const text=nodes.get('behaviour-repeats').textContent;
  assert.match(text,/Worth a look \(1\)/);
  assert.match(text,/Observed, not necessarily wrong \(1\)/);
  assert.match(text,/Not enough evidence to say \(1\)/);
  // The polling tool must appear as activity, never as a loop.
  assert.match(text,/Repeated activity/);
  assert.equal(text.includes('jobs__statusPossible retry loop'),false);
});
test('a failure group states its matching rule and refuses to invent a signature',()=>{
  const {context,nodes}=app();
  context.ov=overview();
  vm.runInContext('renderBehaviourFailures(ov)',context);
  const text=nodes.get('behaviour-failures').textContent;
  assert.match(text,/Grouped because: same tool; same error code -32000/);
  assert.match(text,/does not exist/);
  context.ov.failures[0].fragment="";
  vm.runInContext('renderBehaviourFailures(ov)',context);
  assert.match(nodes.get('behaviour-failures').textContent,/will not invent one for a test/);
});
test('a per-minute rate is never rendered as a percentage',()=>{
  const {context}=app();
  context.rate={unit:"per_minute"};
  assert.equal(vm.runInContext('rateValue(rate,14.8)',context),"14.8 calls/min");
  context.rate={unit:"ratio"};
  assert.equal(vm.runInContext('rateValue(rate,0.667)',context),"66.7%");
  assert.equal(vm.runInContext('rateValue(rate,null)',context),"—");
});
test('undefined denominators read as undefined, never as zero percent',()=>{
  const {context}=app();
  assert.equal(vm.runInContext('rateCell(null,0,"results")',context),"undefined · no results");
  assert.equal(vm.runInContext('rateCell(0,5,"results")',context),"0% of 5 results");
});
test('hostile recorded text in a finding stays literal text',()=>{
  const {context,nodes,tags}=app();
  context.ov=overview();
  context.ov.repeats[0].tool='<img src=x onerror="globalThis.pwned=true">';
  context.ov.failures[0].label='<script>globalThis.pwned=true</script>';
  vm.runInContext('renderBehaviourRepeats(ov);renderBehaviourFailures(ov);',context);
  assert.match(nodes.get('behaviour-repeats').textContent,/<img src=x/);
  assert.match(nodes.get('behaviour-failures').textContent,/<script>/);
  assert.equal(tags.includes('script'),false);
  assert.equal(tags.includes('img'),false);
  assert.equal(context.pwned,undefined);
});
test('the overview renders every section from the API without inventing numbers',()=>{
  const {context,nodes}=app();
  context.ov=overview();
  vm.runInContext('renderBehaviour(ov)',context);
  assert.match(nodes.get('behaviour-stats').textContent,/39Tool calls/);
  assert.match(nodes.get('behaviour-coverage').textContent,/Analysed 8 of 8 recordings/);
  assert.match(nodes.get('behaviour-tools').textContent,/files__read/);
  assert.match(nodes.get('behaviour-denied').textContent,/no-force-push/);
  assert.match(nodes.get('behaviour-trend').textContent,/release 4\.2/);
  assert.match(nodes.get('behaviour-definitions').textContent,/One tools\/call event\./);
  assert.match(nodes.get('behaviour-notes').textContent,/5 calls were never answered/);
});
/* ------------------- M8.1: regression lab controls ------------------- */
// A case result shaped exactly like /api/lab/run returns it.
function labRunBody(status,checks,extra){
  return Object.assign({result:{version:1,title:"Lookup answered",evidence:"04-denials.jsonl",status,checks,note:"Observed MCP boundary only."},comparison_status:"not_requested",command:"tapelog lab run --dir . --case lookup.json"},extra||{});
}
test('an inconclusive lab result is never rendered as a success',()=>{
  const {context,nodes}=app();
  context.body=labRunBody("INCONCLUSIVE",[{kind:"result_never_contains",status:"INCONCLUSIVE",detail:"a call has no recorded result",code:"no_results"}]);
  vm.runInContext('renderCaseResult(body)',context);
  const text=nodes.get('lab-run').textContent;
  assert.match(text,/INCONCLUSIVE/);
  assert.match(text,/This is not a pass/);
  assert.match(text,/No recorded result to judge/);
  // The word "pass" may only appear inside the warning, never as an outcome.
  assert.match(nodes.get('lab-run-target').textContent,/Lookup answered/);
});
test('a failed lab result names the outcome and offers the evidence',()=>{
  const {context,nodes}=app();
  context.body=labRunBody("FAIL",[{kind:"no_attempts",status:"FAIL",detail:"matched 6 attempt(s), want at most 0"}]);
  vm.runInContext('renderCaseResult(body)',context);
  const text=nodes.get('lab-run').textContent;
  assert.match(text,/contradicts the expectation/);
  assert.match(text,/matched 6 attempt\(s\)/);
  assert.match(text,/Open the evidence/);
  assert.match(text,/Reproduce it: tapelog lab run/);
});
test('every expectation kind is explained in words',()=>{
  const {context}=app();
  const read=x=>vm.runInContext('labExpectationText(x)',Object.assign(context,{x}));
  assert.match(read({kind:"denied",tool:"git__push"}),/has a recorded deny decision/);
  assert.match(read({kind:"result_never_contains",tool:"db__query",text:"does not exist"}),/No recorded result of/);
  assert.match(read({kind:"no_attempts",tool:"jobs__status"}),/never reached the boundary/);
  assert.match(read({kind:"sequence",sequence:["a","b"]}),/“a” → “b”/);
  assert.match(read({kind:"attempt_ceiling",tool:"t",maximum:3}),/at most 3/);
  assert.match(read({kind:"no_deny_bypassed"}),/No denied call produced a recorded result/);
  assert.equal(read({kind:"unknown_kind"}),"unknown_kind");
});
test('a suite result separates unusable definitions from inconclusive evidence',()=>{
  const {context,nodes}=app();
  context.body={file:"suite.json",command:"tapelog lab suite --dir . --suite suite.json",suite:{version:1,title:"Nightly",status:"INCONCLUSIVE",pass:1,fail:0,inconclusive:2,invalid:1,cases:[
    {version:1,title:"Good",evidence:"a.jsonl",status:"PASS",checks:[{kind:"answered",status:"PASS",detail:"Expectation satisfied by eligible recorded evidence."}]},
    {version:1,title:"Gone",evidence:"",status:"INCONCLUSIVE",checks:[{kind:"case",status:"INCONCLUSIVE",detail:"the case file cannot be opened",code:"invalid_case"}]}
  ]}};
  vm.runInContext('renderSuiteResult(body)',context);
  const text=nodes.get('lab-run').textContent;
  assert.match(text,/Unusable case definitions/);
  assert.match(text,/The case definition could not be used/);
  assert.match(text,/not a passing suite/);
});
test('a comparison is shown as observation and never as a case outcome',()=>{
  const {context,nodes}=app();
  context.parent=new Node();
  context.delta={new_failures:[{label:"x"}],resolved_failures:[],new_denials:[{tool:"git__push"}],resolved_denials:[],new_repeats:[],resolved_repeats:[],caveats:["Different tasks are not a regression."]};
  vm.runInContext('renderComparison(parent,delta,"complete")',context);
  const text=context.parent.textContent;
  assert.match(text,/New failure groups/);
  assert.match(text,/Different tasks are not a regression/);
  assert.match(text,/never changes the case outcome/);
});
test('hostile recorded text in the lab is literal and never markup',()=>{
  const {context,nodes,tags}=app();
  const evil='<img src=x onerror="globalThis.pwned=true">';
  context.body=labRunBody("FAIL",[{kind:"answered",status:"FAIL",detail:evil}],{});
  context.body.result.title=evil;
  vm.runInContext('renderCaseResult(body)',context);
  assert.match(nodes.get('lab-run').textContent,/<img src=x/);
  assert.equal(tags.includes('img'),false);
  assert.equal(context.pwned,undefined);
});



