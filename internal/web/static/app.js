/* Recording contents are untrusted. Build nodes with textContent only; no
HTML injection, external assets, uploads, or automatic tool execution. */
"use strict";
const $ = id => document.getElementById(id);
function el(tag, cls, text) {
  const n=document.createElement(tag);
  if(cls)n.className=cls;
  if(text!==undefined)n.textContent=String(text);
  return n;
}
function button(text,fn,cls="secondary") {
  const n=el("button",cls,text);
  n.type="button";
  n.onclick=fn;
  return n;
}
function notice(parent,text,kind="") {
  const n=el("div","banner "+kind,text);
  parent.append(n);
  return n;
}
function empty(parent,title,text) {
  parent.replaceChildren();
  const n=el("div","empty");
  n.append(el("div","empty-symbol","▤"),el("h2","",title),el("p","",text));
  parent.append(n);
}
function badge(status) {
  const names= {
    success:"Result recorded",failed:"Tool error",denied:"Denied",unanswered:"Unanswered",confirmation:"Confirmation",deny:"Denied",allow:"Allowed",confirm:"Confirm"
  };
  return el("span","badge "+status,names[status]||status||"Not recorded");
}
function integrity(info) {
  const n=el("span","integrity "+info.integrity);
  const labels= {
    verified:"Chain verified",invalid:"Integrity failure",empty:"Empty recording",limited:"Over viewer limit",not_checked:"Not checked",unavailable:"Unavailable"
  };
  n.textContent=(info.chain_ok?"✓ ":"◇ ")+(labels[info.integrity]||"Not checked");
  return n;
}
function fmtDuration(ms) {
  if(ms===null||ms===undefined)return "Timing unavailable";
  return ms<1000?Math.round(ms)+" ms":ms<60000?(ms/1000).toFixed(2)+" s":(ms/60000).toFixed(1)+" min";
}
function when(ts,full=false) {
  if(!ts)return "Time not recorded";
  const d=new Date(ts);
  return Number.isNaN(d.getTime())?"Invalid timestamp":d.toLocaleString(undefined,full? {
    dateStyle:"medium",timeStyle:"medium"
  }
  : {
    month:"short",day:"numeric",hour:"2-digit",minute:"2-digit"
  }
  );
}
function stats(parent,items) {
  parent.replaceChildren();
  for(const [num,label]of items) {
    const n=el("div","stat");
    n.append(el("div","stat-number",num),el("div","stat-label",label));
    parent.append(n);
  }
}
function error(e) {
  $("error").hidden=false;
  $("error").textContent=e.message||String(e);
}
function clearError() {
  $("error").hidden=true;
}
async function api(path,opts) {
  const res=await fetch(path, {
    cache:"no-store",...opts
  }
  );
  let data;
  try {
    data=await res.json();
  } catch {
    throw new Error("The local workbench returned an unreadable response.");
  }
  if(!res.ok)throw new Error(data.error||("Request failed ("+res.status+")"));
  $("connection").textContent="● Local connection";
  $("connection").className="";
  return data;
}
function params(obj) {
  return new URLSearchParams(obj).toString();
}
function debounce(fn,delay=220) {
  let id;
  return()=> {
    clearTimeout(id);
    id=setTimeout(fn,delay);
  };
}
function pager(id,total,offset,onChange) {
  const n=$(id);
  n.replaceChildren();
  if(total<=50)return;
  n.append(el("span","",(offset+1)+"–"+Math.min(offset+50,total)+" of "+total));
  const prev=button("← Previous",()=>onChange(Math.max(0,offset-50)));
  prev.disabled=offset===0;
  const next=button("Next →",()=>onChange(offset+50));
  next.disabled=offset+50>=total;
  n.append(prev,next);
}
let library=[],directory="",single=false,queueEnabled=false,currentFile="",currentInfo=null,selectedCall=null,currentTab="activity";
let libOffset=0,callOffset=0,eventOffset=0,libraryRequest=0,activityRequest=0,eventRequest=0,detailRequest=0,testRequest=0,compareRequest=0,yamlPreview="";
let detailRevision="";
let selectedEvents=[];
async function loadLibrary() {
  const request=++libraryRequest;
  try {
    const data=await api("/api/sessions?"+params( {
      q:$("session-search").value,status:$("session-status").value,sort:$("session-sort").value,offset:libOffset,limit:50
    }
    ));
    if(request!==libraryRequest)return;
    library=data.sessions||[];
    directory=data.directory;
    single=data.single_session;
    queueEnabled=data.queue_enabled;
    $("approvals-nav").hidden=!queueEnabled;
    $("storage-path").textContent=directory||"No recording configured";
    $("library-count").textContent=data.total;
    stats($("library-stats"),[[data.catalog_total,"Recordings in library"],[library.reduce((n,s)=>n+s.calls,0),"Observed calls · this page"],[library.filter(s=>s.denies||s.failures).length,"Sessions with issues · this page"],[library.filter(s=>s.chain_ok).length,"Verified chains · this page"]]);
    const list=$("session-list");
    list.replaceChildren();
    if(!library.length)empty(list,data.catalog_total?"No matching sessions":"Your next session starts here.",data.catalog_total?"Try a different search or clear the status filter.":"Point tapelog web --dir at your recordings directory. After your client uses an MCP tool, refresh this library.");
    for(const s of library) {
      const row=button("",()=>openSession(s.file),"session-row");
      const title=el("div");
      title.append(el("strong","",s.name),el("small","",(s.harness||"Harness not recorded")+" · "+(s.servers?.join(", ")||"Server not recorded")));
      const state=el("div");
      state.append(integrity(s),el("small","",s.complete?"Recorded end · "+fmtDuration(s.duration_ms):"No recorded end"));
      const counts=el("div","row-counts");
      counts.append(el("span","badge",s.calls+" calls"));
      if(s.denies)counts.append(el("span","badge denied",s.denies+(s.denies===1?" deny decision":" deny decisions")));
      if(s.failures)counts.append(el("span","badge failed",s.failures+(s.failures===1?" tool error":" tool errors")));
      const date=el("div","when",when(s.first_ts));
      row.append(title,state,counts,date);
      row.title=s.file;
      list.append(row);
    }
    if(data.capped)notice(list,"Library file limit reached. Use a more focused recordings directory to browse older sessions.","warning");
    pager("library-pager",data.total,libOffset,n=> {
      libOffset=n;
      loadLibrary();
    }
    );
    if(!currentFile&&location.hash.startsWith("#session=")) {
      try {
        openSession(decodeURIComponent(location.hash.slice(9)),false);
      } catch(e) {
        error(e);
      }
    }
  } catch(e) {
    error(e);
    $("connection").textContent="Offline · refresh to reconnect";
    $("connection").className="connection-offline";
  }
}
function showView(which) {
  for(const name of ["library","session","approvals"])$(name+"-view").hidden=name!==which;
  $("library-nav").classList.toggle("active",which!=="approvals");
  $("approvals-nav").classList.toggle("active",which==="approvals");
}
function showLibrary() {
  ++activityRequest;
  ++eventRequest;
  ++detailRequest;
  ++testRequest;
  ++compareRequest;
  currentFile="";
  selectedCall=null;
  currentInfo=null;
  showView("library");
  history.replaceState(null,"",location.pathname+location.search);
  $("breadcrumb").textContent="Workspace / Sessions";
  clearError();
}
async function openSession(file,updateHash=true) {
  currentFile=file;
  currentInfo=null;
  selectedCall=null;
  selectedEvents=[];
  updateSelectedEvents();
  callOffset=0;
  eventOffset=0;
  detailRevision="";
  ++detailRequest;
  ++eventRequest;
  ++testRequest;
  ++compareRequest;
  $("main").focus( {
    preventScroll:true
  }
  );
  window.scrollTo(0,0);
  clearTest();
  showView("session");
  $("session-title").textContent="Loading session…";
  $("session-subtitle").textContent=file;
  $("inspector").replaceChildren();
  empty($("inspector"),"Follow the evidence","Select a call to see its policy decision, recorded result and related events.");
  $("event-detail").replaceChildren();
  $("compare-result").replaceChildren();
  $("activity-search").value="";
  $("activity-status").value="";
  $("activity-server").replaceChildren(new Option("All servers",""));
  $("fixture-path").value=file;
  $("breadcrumb").textContent="Workspace / Session investigation";
  if(updateHash)history.replaceState(null,"","#session="+encodeURIComponent(file));
  setTab("activity");
  await loadActivity();
}
function showSession(info) {
  const changed=currentInfo&&currentInfo.revision!==info.revision;
  currentInfo=info;
  if(changed) {
    selectedEvents=[];
    updateSelectedEvents();
    selectedCall=null;
    clearTest();
    empty($("inspector"),"Recording changed","Select a call again to inspect the current evidence.");
  }
  $("session-title").textContent=info.name;
  $("session-eyebrow").textContent=info.harness?info.harness.toUpperCase()+" / RECORDED SESSION":"RECORDED SESSION";
  $("session-subtitle").textContent=when(info.first_ts,true)+" · "+info.file;
  stats($("session-stats"),[[info.calls,"Observed calls"],[Math.max(0,info.answered-info.failures),"Non-error results"],[info.denies,"Deny decisions"],[info.failures,"Tool errors"]]);
  const diag=$("session-diagnostics");
  diag.replaceChildren();
  if(!info.chain_ok)notice(diag,info.problem||"This recording has no verified event chain. Comparison and test creation are unavailable.","error");
  else {
    const n=notice(diag,"✓ Hash chain verified for this snapshot. This is an integrity check, not proof of authorship or a complete record of agent activity.");
    n.classList.add("integrity-note");
  }
  if(!info.complete)notice(diag,"No session/end event was recorded. This may be a live session, interruption or failed startup; a missing end alone does not establish a connection failure.","warning");
  if(info.end_reason)notice(diag,"Recorded end reason: "+info.end_reason);
  if(info.calls===0)notice(diag,info.discovery?"Discovery was recorded, but no tool calls crossed this boundary. Explicitly invoke a tool from the wrapped MCP server. Built-in client operations will not appear here.":"No tool calls were recorded. Confirm that your client launches the generated Tapelog entry, then call one of that server’s MCP tools.","warning");
  if(info.ambiguous)notice(diag,"Some request relationships are missing or ambiguous. The timeline does not guess a pairing; inspect All events. Comparison and test creation are disabled.","warning");
  const server=$("activity-server"),prev=server.value;
  server.replaceChildren(new Option("All servers",""));
  for(const name of info.servers||[])server.add(new Option(name,name));
  if([...server.options].some(o=>o.value===prev))server.value=prev;
  $("run-compare").disabled=single;
  $("compare-session").disabled=single;
}
async function loadActivity() {
  const request=++activityRequest,file=currentFile;
  if(!file&&!single)return;
  try {
    const data=await api("/api/activity?"+params( {
      file,q:$("activity-search").value,status:$("activity-status").value,server:$("activity-server").value,offset:callOffset,limit:50
    }
    ));
    if(request!==activityRequest||file!==currentFile)return;
    showSession(data.session);
    const findings=$("investigation-findings");
    findings.replaceChildren();
    if(!data.findings?.length)findings.append(el("p","muted","No linked failure or last-decision denial found in this recording."));
    for(const f of data.findings||[]) findings.append(button(f.label+" · "+f.tool+" →",async()=>{ $("activity-search").value=""; $("activity-status").value=""; $("activity-server").value=""; callOffset=Math.floor(f.index/50)*50; setTab("activity"); await loadActivity(); await selectCall(f.seq); $("inspector").scrollIntoView({block:"nearest"}); }));
    const list=$("activity-list");
    list.replaceChildren();
    $("activity-count").textContent=data.total+" calls";
    if(!data.activities.length)empty(list,"No calls in this view",currentInfo.calls?"Clear an outcome or server filter to see more activity.":"Discovery, session boundaries and other recorded events are available in All events.");
    for(const a of data.activities) {
      const row=button("",()=>selectCall(a.seq),"activity-row"+(selectedCall?.seq===a.seq?" selected":""));
      row.setAttribute("aria-label",a.tool+", "+a.status+", event "+a.seq);
      row.setAttribute("aria-pressed",String(selectedCall?.seq===a.seq));
      const text=el("div");
      text.append(el("span","tool-name",a.tool));
      const meta=el("div","tool-meta");
      meta.append(el("span","",a.server),el("span","",when(a.ts)),el("span","",a.verdict?"Policy: "+a.verdict:"Policy not recorded"));
      text.append(meta);
      const status=el("div","tool-outcome");
      status.append(badge(a.status),el("span","duration",fmtDuration(a.duration_ms)));
      row.append(el("span","seq",String(a.seq).padStart(2,"0")),text,status);
      const wrapper=el("div","activity-choice");
      const pick=el("input","call-checkbox"); pick.type="checkbox"; pick.checked=selectedEvents.some(x=>x.seq===a.seq); pick.setAttribute("aria-label","Select "+a.tool+" event "+a.seq+" for test");
      pick.onchange=()=>{ if(pick.checked) { if(selectedEvents.length>=10){pick.checked=false;error(new Error("Select at most 10 calls."));return;} selectedEvents.push({seq:a.seq,tool:a.tool,status:a.status,result_seq:a.result_seq}); } else selectedEvents=selectedEvents.filter(x=>x.seq!==a.seq); clearTest(); updateSelectedEvents(); if(!selectedCall && pick.checked)selectCall(a.seq); };
      wrapper.append(pick,row); list.append(wrapper);
    }
    pager("activity-pager",data.total,callOffset,n=> {
      callOffset=n;
      loadActivity();
    }
    );
  } catch(e) {
    if(request===activityRequest&&file===currentFile)error(e);
  }
}
function rawDetails(parent,e,open=false) {
  const details=el("details");
  details.open=open;
  details.append(el("summary","","#"+e.seq+" · "+e.type+" · "+when(e.ts)),el("pre","",e.raw));
  if(e.truncated)details.append(el("p","text-warn","Preview capped at 32 KiB. Use the original recording for full contents."));
  parent.append(details);
}
function readableRecorded(value) {
  try { const parsed=JSON.parse(value); if(typeof parsed==="string")return parsed; if(parsed && typeof parsed.message==="string")return parsed.message; if(parsed && Array.isArray(parsed.content)){const texts=parsed.content.filter(x=>x && typeof x.text==="string").map(x=>x.text);if(texts.length)return texts.join("\n");} return JSON.stringify(parsed,null,2); } catch { return String(value); }
}
function dl(parent,values) {
  const n=el("dl");
  for(const[k,v]of values)n.append(el("dt","",k),el("dd","",v));
  parent.append(n);
}
async function selectCall(seq) {
  const request=++detailRequest,file=currentFile;
  try {
    const data=await api("/api/event?"+params( {
      file,seq
    }
    ));
    if(request!==detailRequest||file!==currentFile)return;
    showSession(data.session);
    selectedCall=data.activity;
    detailRevision=data.session.revision;
    clearTest();
    const a=selectedCall,parent=$("inspector");
    parent.replaceChildren();
    if(!a) {
      empty(parent,"Call not found","This event is not a tool call. Use All events to inspect it.");
      return;
    }
    parent.append(el("div","eyebrow","CALL INVESTIGATION / #"+a.seq),el("h2","mono",a.tool),badge(a.status));
    const explanation=a.status==="failed"?"The recorded tool result is marked as an error. Open the result below for the server’s recorded response.":a.reason|| (a.status==="success"?"A result was recorded for this request. A successful response does not independently prove a downstream side effect.":"No matching result was recorded. Do not infer that the server did or did not execute the request.");
    parent.append(el("p","explanation",explanation));
    const evidence=data.evidence||{};
    const evidenceCard=el("section","evidence-card"); evidenceCard.append(el("h3","",a.status==="failed"?"Recorded error":"Recorded evidence"));
    evidenceCard.append(el("p","muted",when(a.ts,true)+" · "+a.server+" · values are as recorded and may contain sensitive information."));
    if(evidence.result!==undefined) evidenceCard.append(el("pre","evidence-value",readableRecorded(evidence.result)));
    else evidenceCard.append(el("p","muted",a.result_seq?"A result is linked but is outside this limited preview. Open All events to inspect it.":"No related result was recorded. This does not establish whether the tool executed."));
    if(evidence.input!==undefined){const input=el("details","evidence-input");input.append(el("summary","","Recorded input"),el("pre","evidence-value",readableRecorded(evidence.input)));evidenceCard.append(input);}
    parent.append(evidenceCard);
    const decisions=evidence.decisions||[];
    const policy=el("section","policy-history");policy.append(el("h3","","Recorded policy decisions"));
    if(!decisions.length)policy.append(el("p","muted","No linked policy decision was recorded."));
    for(const d of decisions){const item=el("div","policy-step");item.append(badge(d.verdict),el("span","", "Decision #"+d.seq+(d.rule?" · rule "+d.rule:"")),el("p","muted",d.reason||"No explanation recorded."));policy.append(item);}
    if(data.related_limited)policy.append(el("p","text-warn","Only the first 20 related events are shown. Inspect All events for the complete history."));
    parent.append(policy);
    if(a.status==="failed") parent.append(el("p","muted","Next: inspect the recorded result and policy decisions below, review preceding context, then compare another verified recording or create a boundary assertion. These are investigation steps, not a diagnosis."));
    if(a.status==="failed"&&a.reason)parent.append(el("p","","Recorded policy explanation: "+a.reason));
    dl(parent,[["Policy",a.verdict||"Not recorded"],["Rule",a.rule||"Not recorded"],["Server",a.server],["Source",a.server_source||"Server identity absent"],["Duration",fmtDuration(a.duration_ms)],["Linked by",a.link]]);
    if(a.verdict==="deny")notice(parent,a.result_seq?"A result is also recorded for this denied call. Investigate the decision/result sequence; denial alone is not evidence of non-execution.":"A deny decision is recorded, with no matched result. This evidence alone does not prove whether a request reached the server.",a.result_seq?"error":"warning");
    if(a.parent_seq)parent.append(button("Open recorded parent #"+a.parent_seq,()=>showRawEvent(a.parent_seq)));
    parent.append(button("Make a regression test →",()=> {
      updateTestSelection();
      setTab("test");
      $("tab-test").focus();
    }
    ,"primary"));
    const observed=el("details");
    observed.append(el("summary","","Arguments, decisions & result (recorded JSON)"),el("p","muted","Recordings may contain secrets. Values are displayed as text; blob references are not fetched."));
    for(const e of data.events)rawDetails(observed,e);
    if(data.related_limited)observed.append(el("p","text-warn","Showing the first 20 related events. Use All events for the rest."));
    parent.append(observed);
    const before=el("details");
    before.append(el("summary","","Preceding context · file order, not causation"));
    for(const e of data.preceding)rawDetails(before,e);
    parent.append(before);
    for(const wrapper of $("activity-list").children) {
      const row=wrapper.children[1]; if(!row)continue;
      const yes=row.getAttribute("aria-label")?.endsWith("event "+a.seq);
      row.classList.toggle("selected",!!yes);
      row.setAttribute("aria-pressed",String(!!yes));
    }
    updateTestSelection();
  } catch(e) {
    if(request===detailRequest&&file===currentFile)error(e);
  }
}
async function loadEvents() {
  const file=currentFile,request=++eventRequest;
  try {
    const data=await api("/api/events?"+params( {
      file,offset:eventOffset,limit:50,summary:1
    }
    ));
    if(file!==currentFile||request!==eventRequest)return;
    showSession(data.session);
    const list=$("events-list");
    list.replaceChildren();
    for(const e of data.events) {
      const details=el("details","recorded-event"),row=el("summary","event-row");
      row.append(el("span","seq","#"+e.seq),el("span","mono",when(e.ts)),el("span","",e.type));
      details.append(row);
      details.ontoggle=async()=>{ if(!details.open || details.dataset.loaded)return; try { const full=await api("/api/event?"+params({file,seq:e.seq})); if(file!==currentFile || full.session.revision!==data.session.revision) { error(new Error("Recording changed; refresh events before inspecting.")); return; } const found=full.events.find(x=>x.seq===e.seq); if(found) { details.append(el("pre","",found.raw)); details.dataset.loaded="yes"; } } catch(err) { error(err); } };
      list.append(details);
    }
    if(!data.events.length)empty(list,"No readable events","This recording is empty or could not be parsed. Review its integrity message.");
    pager("events-pager",data.total,eventOffset,n=> {
      eventOffset=n;
      loadEvents();
    }
    );
  } catch(e) {
    if(request===eventRequest&&file===currentFile)error(e);
  }
}
async function showRawEvent(seq) {
  const file=currentFile;
  try {
    const data=await api("/api/event?"+params( {
      file,seq
    }
    ));
    if(file!==currentFile)return;
    setTab("events");
    $("event-detail").replaceChildren();
    for(const e of data.events)rawDetails($("event-detail"),e,true);
  } catch(e) {
    error(e);
  }
}
function setTab(name) {
  currentTab=name;
  for(const tab of document.querySelectorAll(".tab")) {
    const selected=tab.dataset.tab===name;
    tab.classList.toggle("selected",selected);
    tab.setAttribute("aria-selected",String(selected));
    tab.tabIndex=selected?0:-1;
    $(tab.dataset.tab+"-panel").hidden=!selected;
  }
  if(name==="events")loadEvents();
  if(name==="compare")loadCompareChoices();
  if(name==="test")updateTestSelection();
}
async function loadCompareChoices() {
  const file=currentFile,request=++compareRequest;
  try {
    const data=await api("/api/sessions?limit=100");
    if(data.total>100){const older=await api("/api/sessions?offset=100&limit=100");data.sessions.push(...older.sessions);}
    if(request!==compareRequest||file!==currentFile)return;
    const select=$("compare-session");
    const previous=select.value;
    select.replaceChildren();
    for(const s of data.sessions||[]) {
      if(s.file===file)continue;
      select.add(new Option(s.name+" · "+when(s.first_ts),s.file));
    }
    if([...select.options].some(o=>o.value===previous))select.value=previous;
    else $("compare-result").replaceChildren();
    if(single||!select.options.length) {
      select.add(new Option("Choose another recording in directory mode",""));
      $("run-compare").disabled=true;
    } else $("run-compare").disabled=false;
  } catch(e) {
    error(e);
  }
}
async function compare() {
  const file=currentFile,other=$("compare-session").value,request=++compareRequest;
  const out=$("compare-result");
  out.replaceChildren();
  $("run-compare").disabled=true;
  try {
    const data=await api("/api/compare?"+params( {
      a:file,b:other
    }
    ));
    if(file!==currentFile||request!==compareRequest)return;
    const inv=data.investigation;
    notice(out,inv.change_count||inv.sequence_differs?"Recorded boundary activity differs.":"No difference found in the compared boundary fields.",inv.change_count||inv.sequence_differs?"warning":"");
    out.append(el("p","","Attempts: "+inv.attempts_a+" / "+inv.attempts_b+" · "+inv.change_count+" outcome or count differences"));
    if(inv.ambiguous_duplicates)notice(out,"Repeated identical call keys are paired by occurrence; the pairing is uncertain.","warning");
    const differences=el("ul","mono"); for(const change of inv.changes)differences.append(el("li","",change)); out.append(differences);
    if(inv.change_count>100)out.append(el("p","muted","Showing first 100 differences."));
    out.append(el("p","muted",inv.sequence_differs?"Attempt sequence differs (including added or missing attempts).":"Attempt sequence matches."));
    out.append(el("p","muted",data.scope));
    const history=data.policy_history; if(history){const section=el("section","policy-history");section.append(el("h3","","Policy decision history"),el("p","muted",history.total+" matched call histories changed (verdict, rule or reason). Matching shares the same occurrence uncertainty."));for(const change of history.changes)section.append(el("p","mono",change));if(history.total>100)section.append(el("p","muted","Showing the first 100 changes."));out.append(section);}
    const cols=el("div","comparison-columns");
    for(const [side,title]of [[data.a,"This recording"],[data.b,"Compared recording"]]) {
      const n=el("div");
      n.append(el("h3","",title),el("p","",side.name),el("p","muted",side.answered+" results · "+side.denies+" deny decisions · "+side.failures+" tool errors"));
      cols.append(n);
    }
    out.append(cols);
    for(const[key,title]of [["only_in_a","Only in this recording"],["only_in_b","Only in compared recording"],["changed","Same call key, different recorded outcomes / count"]]) {
      const n=el("div");
      n.append(el("h3","",title));
      const list=el("ul","mono");
      for(const text of (data.diff[key]||[]).slice(0,100))list.append(el("li","",String(text).slice(0,512)));
      if((data.diff[key]||[]).length>100)list.append(el("li","muted","Showing the first 100 entries."));
      if(!list.children.length)list.append(el("li","muted","None"));
      n.append(list);
      out.append(n);
    }
    out.append(el("p","muted",data.diff.order_differs?"Answered call sequence differs. Added or missing calls also change this sequence.":"Answered call sequence is unchanged."));
  } catch(e) {
    if(file===currentFile&&request===compareRequest)notice(out,e.message,"warning");
  } finally {
    if(file===currentFile&&request===compareRequest)$("run-compare").disabled=false;
  }
}
function clearTest() {
  ++testRequest;
  yamlPreview="";
  $("test-yaml").hidden=true;
  $("test-yaml").textContent="";
  $("save-test").disabled=true;
  $("test-result").replaceChildren();
}
function updateSelectedEvents() {
  const summary=$("activity-selection"), list=$("selected-events");
  summary.textContent=selectedEvents.length+" selected";
  list.replaceChildren();
  if(!selectedEvents.length)list.append(el("p","muted","No calls selected for a sequence. Choose calls in Activity."));
  selectedEvents.forEach((item,i)=>{const row=el("div","selected-event");row.append(el("span","selection-index",String(i+1)),el("strong","mono",item.tool),el("span","muted",item.result_seq?"Result recorded":"No result"));
    const up=button("↑",()=>moveSelected(i,-1));up.disabled=i===0;up.setAttribute("aria-label","Move "+item.tool+" earlier");
    const down=button("↓",()=>moveSelected(i,1));down.disabled=i===selectedEvents.length-1;down.setAttribute("aria-label","Move "+item.tool+" later");
    const remove=button("Remove",()=>{selectedEvents.splice(i,1);clearTest();updateSelectedEvents();}); row.append(up,down,remove);list.append(row);
  });
}
function moveSelected(i,delta) { const j=i+delta;if(j<0||j>=selectedEvents.length)return;[selectedEvents[i],selectedEvents[j]]=[selectedEvents[j],selectedEvents[i]];clearTest();updateSelectedEvents(); }
function updateTestSelection() {
  $("test-selection").textContent=selectedCall?"Event #"+selectedCall.seq+" · "+selectedCall.tool+" · "+selectedCall.status:"Select a call in Activity first.";
  $("preview-test").disabled=!selectedCall;
  $("run-test").disabled=!selectedCall;
}
async function generateTest(run) {
  clearTest();
  if(!selectedCall) {
    notice($("test-result"),"Select a call in Activity first.","warning");
    return;
  }
  if(!$("assertion-kind").value) { notice($("test-result"),"Choose a regression rule first.","warning"); return; }
  if($("assertion-kind").value==="attempt_ceiling" && ($("attempt-ceiling").value.trim()==="" || !Number.isInteger(Number($("attempt-ceiling").value)) || Number($("attempt-ceiling").value)<0 || Number($("attempt-ceiling").value)>100000)) { notice($("test-result"),"Enter a whole-number attempt ceiling from 0 to 100000.","warning"); return; }
  const file=currentFile,seq=selectedCall.seq,request=++testRequest;
  try {
    const data=await api("/api/test", {
      method:"POST",headers: {
        "Content-Type":"application/json"
      }
      ,body:JSON.stringify( {
        file,revision:detailRevision,seq,kind:$("assertion-kind").value,fixture:$("fixture-path").value,seqs:selectedEvents.map(x=>x.seq),ceiling:$("assertion-kind").value==="attempt_ceiling"?Number($("attempt-ceiling").value):null,run
      }
      )
    }
    );
    if(request!==testRequest||file!==currentFile||seq!==selectedCall?.seq)return;
    yamlPreview=data.yaml;
    $("test-yaml").textContent=data.yaml;
    $("test-yaml").hidden=false;
    $("save-test").disabled=false;
    notice($("test-result"),run?(data.passed?"✓ Assertion passed using the existing test engine. No tool was executed.":"Assertion failed. Review the recorded evidence."):"Preview ready. Review the assertion before saving.",run&&!data.passed?"error":"");
    $("test-result").append(el("p","muted",data.scope));
    for(const f of data.failures||[])$("test-result").append(el("p","text-bad",f.Detail));
  } catch(e) {
    if(request===testRequest&&file===currentFile)notice($("test-result"),e.message,"warning");
  }
}
async function saveTest() {
  if(!yamlPreview)return;
  const text=yamlPreview;
  try {
    if(window.showSaveFilePicker) {
      const handle=await window.showSaveFilePicker( {
        suggestedName:"tapelog-regression.yaml",types:[ {
          description:"Tapelog trajectory test",accept: {
            "text/yaml":[".yaml"]
          }
        }
        ]
      }
      );
      const existing=await handle.getFile();
      if(existing.size>0)throw new Error("That file already contains data. Choose a new filename to preserve it.");
      const stream=await handle.createWritable();
      await stream.write(text);
      await stream.close();
      notice($("test-result"),"Saved the test. The recording was not copied or modified.");
    } else {
      const url=URL.createObjectURL(new Blob([text], {
        type:"text/yaml"
      }
      ));
      const a=el("a");
      a.href=url;
      a.download="tapelog-regression-"+Date.now()+".yaml";
      a.click();
      setTimeout(()=>URL.revokeObjectURL(url),1000);
      notice($("test-result"),"Test downloaded. Your browser controls the save location; check the fixture path before running tapelog test.");
    }
  } catch(e) {
    if(e.name!=="AbortError")notice($("test-result"),e.message,"warning");
  }
}
function shellArg(s) {
  return /Win/.test(navigator.platform)?"'"+String(s).replaceAll("'","''")+"'":"'"+String(s).replaceAll("'","'\\''")+"'";
}
function fullPath() {
  return directory+(directory.endsWith("/")||directory.endsWith("\\")?"":/Win/.test(navigator.platform)?"\\":"/")+currentFile;
}
function help(replay=false) {
  $("help-title").textContent=replay?"Replay recorded responses, safely.":"Bring your sessions into view.";
  const out=$("help-content");
  out.replaceChildren();
  if(replay) {
    out.append(el("p","","Replay starts a hermetic MCP server and waits for client requests. It serves matching recorded responses; it does not re-run the upstream tool. Denied or unanswered calls are replay misses."),el("pre","","tapelog replay "+shellArg(fullPath())+" --strict"),el("p","","Inspect and verify the original recording:"),el("pre","","tapelog inspect "+shellArg(fullPath())+" --summary\ntapelog verify "+shellArg(fullPath())));
    if(!currentInfo?.chain_ok)notice(out,"This recording has not passed chain verification. Replay refuses altered evidence by default.","warning");
  } else {
    out.append(el("p","","Use setup to preview a configuration for an existing MCP server, save a new candidate and review it before activation. Generated configs create a fresh recording on each start."),el("pre","","tapelog setup --client roo --config <client.json> --server <name>\ntapelog web --dir <recordings-directory>"),el("p","","Restart the wrapped server in your client, explicitly call one of its MCP tools, then refresh Sessions. Tapelog cannot see the client’s built-in file, terminal or browser operations."),el("p","","Use Activity to investigate policy decisions and failures. All events preserves file order. Compare uses recorded responses. Make a test creates a tool-level assertion without copying payloads."));
  }
  $("help-dialog").showModal();
}
let approvalsBusy=false;
async function refreshPending() {
  if(!queueEnabled||approvalsBusy)return;
  if($("pending-list").contains(document.activeElement))return;
  try {
    const data=await api("/api/pending");
    $("pending-count").textContent=data.pending.length;
    const out=$("pending-list");
    out.replaceChildren();
    if(!data.pending.length)empty(out,"No calls waiting for review","A policy confirmation request will appear here when the live recording parks a call.");
    for(const item of data.pending) {
      const card=el("div","pending-card");
      card.append(el("h2","mono",item.tool),el("p","muted","Request #"+item.id+" · waiting "+item.age_seconds+" seconds"),el("pre","",JSON.stringify(item.args,null,2).slice(0,32768)));
      const actions=el("div","actions"),note=el("input");
      note.placeholder="Review note (recorded)";
      note.setAttribute("aria-label","Review note for request "+item.id);
      actions.append(note);
      for(const[v,label]of [["allow","Allow once"],["allow_session","Allow for session"],["deny","Deny"]])actions.append(button(label,()=>decide(item.id,v,note.value),v==="deny"?"danger":"secondary"));
      card.append(actions);
      out.append(card);
    }
  } catch(e) {
    error(e);
  }
}
async function decide(id,verdict,note) {
  approvalsBusy=true;
  try {
    await api("/api/decide", {
      method:"POST",headers: {
        "Content-Type":"application/json"
      }
      ,body:JSON.stringify( {
        id,verdict,note
      }
      )
    }
    );
  } catch(e) {
    error(e);
  } finally {
    approvalsBusy=false;
    await refreshPending();
  }
}
$("library-nav").onclick=showLibrary;
$("back").onclick=showLibrary;
$("approvals-nav").onclick=()=> {
  showView("approvals");
  refreshPending();
};
$("help-nav").onclick=()=>help();
$("replay-help").onclick=()=>help(true);
$("close-help").onclick=()=>$("help-dialog").close();
$("refresh").onclick=()=> {
  clearError();
  if(!$("session-view").hidden) {
    if(currentTab==="events")loadEvents();
    else loadActivity();
  } else if(!$("approvals-view").hidden)refreshPending();
  else loadLibrary();
};
$("library-filters").onsubmit=e=> {
  e.preventDefault();
  libOffset=0;
  loadLibrary();
};
$("session-search").oninput=debounce(()=> {
  libOffset=0;
  loadLibrary();
}
);
for(const id of ["session-status","session-sort"])$(id).onchange=()=> {
  libOffset=0;
  loadLibrary();
};
$("activity-search").oninput=debounce(()=> {
  callOffset=0;
  loadActivity();
}
);
for(const id of ["activity-status","activity-server"])$(id).onchange=()=> {
  callOffset=0;
  loadActivity();
};
for(const tab of document.querySelectorAll(".tab")) {
  tab.onclick=()=>setTab(tab.dataset.tab);
  tab.onkeydown=e=> {
    const tabs=[...document.querySelectorAll(".tab")];
    let i=tabs.indexOf(tab);
    if(e.key==="ArrowRight")i=(i+1)%tabs.length;
    else if(e.key==="ArrowLeft")i=(i+tabs.length-1)%tabs.length;
    else if(e.key==="Home")i=0;
    else if(e.key==="End")i=tabs.length-1;
    else return;
    e.preventDefault();
    tabs[i].focus();
    setTab(tabs[i].dataset.tab);
  };
}
$("run-compare").onclick=compare;
$("compare-session").onchange=()=>{++compareRequest;$("compare-result").replaceChildren();$("run-compare").disabled=single||!$("compare-session").value;};
$("preview-test").onclick=()=>generateTest(false);
$("run-test").onclick=()=>generateTest(true);
$("save-test").onclick=saveTest;
$("assertion-kind").onchange=()=>{
  clearTest(); const kind=$("assertion-kind").value; $("ceiling-field").hidden=kind!=="attempt_ceiling";
  const descriptions={denied:"At least one recorded decision for this tool must be deny.",attempted:"At least one attempt must reach this recorded boundary.",answered:"At least one call to this tool must have a recorded result.",never_answered:"No recorded result for this tool; this does not prove non-execution.",no_attempts:"No attempts to this tool may appear at the recorded boundary.",sequence:"The selected answered tool names must appear in the displayed order, as a subsequence.",invariant:"Checks for recorded deny decisions with recorded results; it cannot observe actions outside this boundary.",attempt_ceiling:"Counts all recorded boundary attempts for the selected tool, including unanswered calls."};
  $("assertion-help").textContent=descriptions[kind]||"Choose a rule that reflects the behaviour you want to test.";
};
$("fixture-path").oninput=clearTest;
$("attempt-ceiling").oninput=clearTest;
$("choose-events").onclick=()=>{setTab("activity");loadActivity();};
$("selection-to-test").onclick=()=>{updateSelectedEvents();setTab("test");$("tab-test").focus();};
let light=false;
try {
  light=localStorage.getItem("tapelog-appearance")==="light";
} catch {
}
function applyTheme() {
  document.documentElement.dataset.theme=light?"light":"dark";
  $("theme").textContent=light?"◐ Dark appearance":"◐ Light appearance";
}
applyTheme();
$("theme").onclick=()=> {
  light=!light;
  applyTheme();
  try {
    localStorage.setItem("tapelog-appearance",light?"light":"dark");
  } catch {
  }
};
window.addEventListener("hashchange",()=> {
  if(location.hash==="#main")return;
  if(location.hash.startsWith("#session="))openSession(decodeURIComponent(location.hash.slice(9)),false);
  else showLibrary();
}
);
loadLibrary().then(()=>refreshPending());
setInterval(refreshPending,3000);
