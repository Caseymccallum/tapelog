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

