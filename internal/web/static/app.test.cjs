/* Node's built-in runner; no new runtime/test dependencies. The small DOM
   adapter rejects HTML sinks and exercises the real rendering functions. */
const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
class Node {
 constructor(tag='div'){this.tagName=tag;this.children=[];this.attributes={};this.value='';this.hidden=false;this.className='';this.classList={add:()=>{},toggle:()=>{}};}
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
