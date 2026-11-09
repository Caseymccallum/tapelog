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
  const symbol=el("div","empty-symbol","▤");
  symbol.setAttribute("aria-hidden","true");
  n.append(symbol,el("h2","",title),el("p","",text));
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
function groupDigits(value) {
  // Numbers in a dense table are read by comparing digit groups, so group them.
  if(typeof value!=="number"||!Number.isFinite(value))return value;
  return value.toLocaleString("en-GB");
}
function stats(parent,items) {
  parent.replaceChildren();
  // A stat is only legible as a strip: four bare .stat blocks used to stack as
  // full-width rows in any container that forgot the wrapper class. A fragment
  // or a test double has no classList, so the guard stays optional.
  if(parent.classList&&typeof parent.classList.contains==="function"&&!parent.classList.contains("stat-strip"))parent.classList.add("stat-strip");
  for(const [num,label]of items) {
    const n=el("div","stat");
    n.append(el("div","stat-number",groupDigits(num)),el("div","stat-label",label));
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
let labPreview="";
let selectedEvents=[];
let inspectedEvent=0;
async function loadLibrary() {
  const request=++libraryRequest;
  try {
    const data=await api("/api/sessions?"+params( {
      q:$("session-search").value,status:$("session-status").value,sort:$("session-sort").value,offset:libOffset,limit:50
    }
    ));
    if(request!==libraryRequest)return;
    library=data.sessions||[];
    const recordingOptions=$("recording-options");
    recordingOptions.replaceChildren();
    for(const item of library)recordingOptions.append(new Option(item.name+" · "+item.file,item.file));
    directory=data.directory;
    single=data.single_session;
    queueEnabled=data.queue_enabled;
    $("approvals-nav").hidden=!queueEnabled;
    $("storage-path").textContent=directory||"No recording configured";
    $("library-count").textContent=data.total;
    stats($("library-stats"),[[data.catalog_total,"Recordings in library"],[library.reduce((n,s)=>n+s.calls,0),"Observed calls · this page"],[library.filter(s=>s.denies||s.failures).length,"Sessions with issues · this page"],[library.filter(s=>s.chain_ok).length,"Verified chains · this page"]]);
    const list=$("session-list");
    list.replaceChildren();
    if(library.length) {
      const head=el("div","session-head");
      for(const label of ["Session","Recording","Signals","Recorded"])head.append(el("span","",label));
      list.append(head);
    }
    if(!library.length)empty(list,data.catalog_total?"No matching sessions":"Your next session starts here.",data.catalog_total?"Try a different search or clear the status filter.":"Point tapelog web --dir at your recordings directory. After your client uses an MCP tool, refresh this library.");
    for(const s of library) {
      const row=button("",()=>openSession(s.file),"session-row");
      const title=el("div");
      title.append(el("strong","",s.name),el("small","",(s.harness||"Harness not recorded")+" · "+(s.servers?.join(", ")||"Server not recorded")));
      const state=el("div");
      state.append(integrity(s),el("small","",s.complete?"Recorded end · "+fmtDuration(s.duration_ms):"No recorded end"));
      const counts=el("div","row-counts");
      counts.append(el("span","badge",s.calls+(s.calls===1?" call":" calls")));
      if(s.ambiguous)counts.append(el("span","badge ambiguous","pairing unknown"));
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
  } catch(e) {
    error(e);
    $("connection").textContent="Offline · refresh to reconnect";
    $("connection").className="connection-offline";
  }
}
// hashTarget reads a deep link of the form #session=<file>&tab=<tab>. A tab
// deep link makes an investigation shareable and lets a reviewer open the same
// view twice.
function hashTarget() {
  if(!location.hash.startsWith("#session="))return null;
  const query=new URLSearchParams(location.hash.slice(1));
  const file=query.get("session");
  return {
    file,tab:query.get("tab"),seq:validEventSeq(query.get("seq"))
  };
}
function showView(which) {
  if(activeView!==which) {
    window.scrollTo(0,0);
    $("main").focus({preventScroll:true});
  }
  if(which!=="lab")++labRunRequest;
  activeView=which;
  for(const name of ["library","investigations","comparisons","behaviour","lab","session","studio","approvals"])$(name+"-view").hidden=name!==which;
  $("library-nav").classList.toggle("active",which==="library");
  $("behaviour-nav").classList.toggle("active",which==="behaviour");
  $("lab-nav").classList.toggle("active",which==="lab");
  $("studio-nav").classList.toggle("active",which==="studio");
  $("approvals-nav").classList.toggle("active",which==="approvals");
  $("investigations-nav").classList.toggle("active",which==="investigations"||which==="session");
  $("comparisons-nav").classList.toggle("active",which==="comparisons");
  $("suites-nav").classList.toggle("active",which==="lab"&&labTab==="suites");
  updateWorkspaceContext();
}
function showLibrary() {
  ++activityRequest;
  ++eventRequest;
  ++detailRequest;
  ++testRequest;
  ++compareRequest;
  showView("library");
  setWorkspaceRoute("#sessions");
  $("breadcrumb").textContent="Workspace / Sessions";
  clearError();
}
async function openSession(file,updateHash=true) {
  const deepLink=hashTarget();
  if(file!==currentFile) {
    for(const id of ["lab-title-input","lab-description","lab-reference","lab-candidate"])$(id).value="";
    $("assertion-kind").value="";
  }
  currentFile=file;
  currentInfo=null;
  selectedCall=null;
  inspectedEvent=0;
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
  $("activity-list").replaceChildren(el("p","muted","Opening recorded calls…"));
  $("session-stats").replaceChildren();
  $("session-diagnostics").replaceChildren();
  $("investigation-findings").replaceChildren();
  $("inspector").replaceChildren();
  empty($("inspector"),"Follow the evidence","Select a call to see its policy decision, recorded result and related events.");
  $("event-detail").replaceChildren();
  $("compare-result").replaceChildren();
  $("activity-search").value="";
  $("activity-status").value="";
  $("activity-server").replaceChildren(new Option("All servers",""));
  $("fixture-path").value=file;
  $("breadcrumb").textContent="Sessions / "+file;
  if(updateHash)setWorkspaceRoute(sessionHash(file,"activity",0));
  setTab("activity");
  await loadActivity();
  if(currentFile!==file)return;
  if(deepLink&&deepLink.file===file) {
    if(deepLink.tab)setTab(deepLink.tab);
    if(deepLink.seq) {
      if(deepLink.tab==="events")await showRawEvent(deepLink.seq);
      else await selectCall(deepLink.seq);
    }
  }
  updateWorkspaceContext();
}
// structureNotice separates the structure verdict from the chain verdict. A
// verified chain says nothing about whether the file is one usable session, and
// this surface is where a reader decides what the evidence is.
function structureNotice(info) {
  const problems=(info.structure_problems||[]).filter(Boolean);
  const detail=problems.length?" ("+problems.join("; ")+")":"";
  if(info.structure==="invalid")return {
    tone:"error",
    text:"This recording is not one valid session"+detail+". Its hash chain may still be intact — integrity and structure are different questions — but it cannot establish a single complete session, comparison is unavailable, and a case that depends on one session will be inconclusive."
  };
  // no_end is already explained by the completeness banner; only unusual
  // structural problems need their own sentence.
  const unusual=problems.filter(p=>p!=="no_end");
  if(info.structure==="incomplete"&&unusual.length)return {
    tone:"warning",
    text:"This recording is structurally incomplete"+detail+". The chain may be intact; the missing structure is a separate defect."
  };
  return null;
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
  let healthWarning=false;
  if(!info.chain_ok)notice(diag,info.problem||"This recording has no verified event chain. Comparison and test creation are unavailable.","error");
  else {
    const n=notice(diag,"✓ Hash chain verified for this snapshot. This is an integrity check, not proof of authorship or a complete record of agent activity.");
    n.classList.add("integrity-note");
  }
  if(!info.chain_ok)healthWarning=true;
  if(!info.complete){notice(diag,"No session/end event was recorded. This may be a live session, interruption or failed startup; a missing end alone does not establish a connection failure.","warning");healthWarning=true;}
  const structure=structureNotice(info);
  if(structure){notice(diag,structure.text,structure.tone);healthWarning=true;}
  if(info.end_reason)notice(diag,"Recorded end reason: "+info.end_reason);
  if(info.calls===0){notice(diag,info.discovery?"Discovery was recorded, but no tool calls crossed this boundary. Explicitly invoke a tool from the wrapped MCP server. Built-in client operations will not appear here.":"No tool calls were recorded. Confirm that your client launches the generated Tapelog entry, then call one of that server’s MCP tools.","warning");healthWarning=true;}
  if(info.ambiguous){notice(diag,"Some request relationships are missing or ambiguous. The timeline does not guess a pairing: a result whose owner is unknown is attached to neither call, comparison is unavailable, and any case that depends on which call produced a result will be inconclusive.","warning");healthWarning=true;}
  // A warning nobody opens is a warning nobody reads: the banners live inside
  // the collapsed "Recording integrity & scope" disclosure.
  const health=document.querySelector("details.recording-health");
  if(healthWarning&&health)health.open=true;
  const server=$("activity-server"),prev=server.value;
  server.replaceChildren(new Option("All servers",""));
  for(const name of info.servers||[])server.add(new Option(name,name));
  if([...server.options].some(o=>o.value===prev))server.value=prev;
  $("run-compare").disabled=single||!!info.ambiguous;
  $("compare-session").disabled=single||!!info.ambiguous;
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
      // Keep internal selection identity separate from the native visible name.
      row.dataset.seq=String(a.seq);
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
    if(request===activityRequest&&file===currentFile) {
      error(e);
      $("session-title").textContent=file;
      $("session-subtitle").textContent="Recording unavailable. The evidence may have moved or be outside the viewer limits.";
      empty($("activity-list"),"The recording could not be opened","Return to the case or choose another recording. No outcome can be inferred from missing evidence.");
    }
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
  try { const parsed=JSON.parse(value); if(typeof parsed==="string")return parsed; if(parsed && typeof parsed.message==="string")return parsed.message; if(parsed?.error && typeof parsed.error.message==="string")return parsed.error.message; if(parsed && Array.isArray(parsed.content)){const texts=parsed.content.filter(x=>x && typeof x.text==="string").map(x=>x.text);if(texts.length)return texts.join("\n");} return JSON.stringify(parsed,null,2); } catch { return String(value); }
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
    inspectedEvent=seq;
    detailRevision=data.session.revision;
    clearTest();
    const a=selectedCall,parent=$("inspector");
    parent.replaceChildren();
    if(!a) {
      empty(parent,"Call not found","This event is not a tool call. Use All events to inspect it.");
      return;
    }
    if(Number.isInteger(data.activity_index)&&data.activity_index>=0&&!$("activity-search").value&&!$("activity-status").value&&!$("activity-server").value) {
      const page=Math.floor(data.activity_index/50)*50;
      if(page!==callOffset) { callOffset=page;await loadActivity(); }
      if(request!==detailRequest||file!==currentFile)return;
    }
    parent.append(el("div","eyebrow","CALL INVESTIGATION / #"+a.seq),el("h2","mono",a.tool),badge(a.status));
    const explanation=a.status==="failed"?"The recorded tool result is marked as an error. Open the result below for the server’s recorded response.":a.reason|| (a.status==="success"?"A result was recorded for this request. A successful response does not independently prove a downstream side effect.":"No matching result was recorded. Do not infer that the server did or did not execute the request.");
    parent.append(el("p","explanation",explanation));
    parent.append(callActionBar(a));
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
      const yes=row.dataset.seq===String(a.seq);
      row.classList.toggle("selected",!!yes);
      row.setAttribute("aria-pressed",String(!!yes));
    }
    updateTestSelection();
    $("breadcrumb").textContent="Sessions / "+(currentInfo?.name||file)+" / Event #"+a.seq;
    updateWorkspaceContext();
    if(activeView==="session")setWorkspaceRoute(sessionHash(file,currentTab,a.seq));
    if(activeView==="session"&&currentTab==="activity")$("inspector").scrollIntoView({block:"nearest"});
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
      details.ontoggle=async()=>{ if(!details.open)return; inspectedEvent=e.seq;updateWorkspaceContext();setWorkspaceRoute(sessionHash(file,"events",e.seq));if(details.dataset.loaded)return; try { const full=await api("/api/event?"+params({file,seq:e.seq})); if(file!==currentFile || full.session.revision!==data.session.revision) { error(new Error("Recording changed; refresh events before inspecting.")); return; } const found=full.events.find(x=>x.seq===e.seq); if(found) { details.append(el("pre","",found.raw)); details.dataset.loaded="yes"; } } catch(err) { error(err); } };
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
    inspectedEvent=seq;
    updateWorkspaceContext();
    setWorkspaceRoute(sessionHash(file,"events",seq));
    $("event-detail").replaceChildren();
    for(const e of data.events)rawDetails($("event-detail"),e,true);
  } catch(e) {
    error(e);
  }
}
function setTab(name) {
  if(name==="compare") { showComparisons(currentFile); return; }
  if(!["activity","events","repeats","test"].includes(name))name="activity";
  currentTab=name;
  for(const tab of document.querySelectorAll("[data-tab]")) {
    // Only session-workspace tabs own a *-panel id. The regression lab uses its
    // own data-labtab tabs and must not be swept up here.
    if(!tab.dataset.tab)continue;
    const selected=tab.dataset.tab===name;
    tab.classList.toggle("selected",selected);
    tab.setAttribute("aria-selected",String(selected));
    tab.tabIndex=selected?0:-1;
    $(tab.dataset.tab+"-panel").hidden=!selected;
  }
  if(name==="events")loadEvents();
  if(name==="compare")loadCompareChoices();
  if(name==="repeats")loadRepeats();
  if(name==="test")updateTestSelection();
  if(activeView==="session")setWorkspaceRoute(sessionHash(currentFile,name,name==="events"?inspectedEvent:selectedCall?.seq));
}
async function loadCompareChoices() {
  const file=currentFile,request=++compareRequest;
  try {
    const data=await api("/api/sessions?limit=100");
    if(data.total>100){const older=await api("/api/sessions?offset=100&limit=100");data.sessions.push(...older.sessions);}
    if(request!==compareRequest||file!==currentFile)return;
    const base=$("compare-base"),previousBase=base.value;
    base.replaceChildren(new Option("Choose a reference recording",""));
    for(const s of data.sessions||[])base.add(new Option(s.name+" · "+s.file,s.file));
    const preferred=pendingComparison?.a||previousBase||file;
    if([...base.options].some(o=>o.value===preferred))base.value=preferred;
    workspaceRecordings=data.sessions||[];
    const select=$("compare-session");
    const previous=select.value;
    select.replaceChildren();
    for(const s of data.sessions||[]) {
      if(s.file===base.value)continue;
      select.add(new Option(s.name+" · "+s.file,s.file));
    }
    if([...select.options].some(o=>o.value===(pendingComparison?.b||previous)))select.value=pendingComparison?.b||previous;
    else $("compare-result").replaceChildren();
    if(single||!select.options.length) {
      select.add(new Option("Choose another recording in directory mode",""));
      $("run-compare").disabled=true;
    } else $("run-compare").disabled=false;
    syncComparisonPair();
  } catch(e) {
    error(e);
  }
}
async function compare() {
  const file=compareBaseFile(),other=$("compare-session").value,request=++compareRequest;
  const out=$("compare-result");
  out.replaceChildren();
  $("run-compare").disabled=true;
  try {
    const data=await api("/api/compare?"+params( {
      a:file,b:other
    }
    ));
    if(file!==compareBaseFile()||request!==compareRequest)return;
    const inv=data.investigation;
    const differs=!!(inv.change_count||inv.sequence_differs);
    // The verdict is one line with a status word, not a banner: a difference is
    // the normal result of comparing two runs, so it must not read as an alarm.
    const verdict=el("div","compare-verdict "+(differs?"differs":"same"));
    verdict.append(el("span","compare-verdict-word",differs?"Differs":"No change"));
    const facts=el("p","");
    facts.append(el("span","",inv.attempts_a+" → "+inv.attempts_b+" recorded attempts"),el("span","compare-sep"," · "),el("span","",inv.change_count+" outcome or count difference"+(inv.change_count===1?"":"s")));
    verdict.append(facts);
    verdict.append(el("p","muted",inv.sequence_differs?"The attempt sequence differs, including calls that were added or removed.":"The attempt sequence matches."));
    out.append(verdict);
    if(inv.ambiguous_duplicates)notice(out,"Some call keys repeat, so pairing is by occurrence. That pairing is uncertain and no sentence below depends on it being right.","warning");
    const pair=el("div","compare-pair");
    for(const [side,title]of [[data.a,"Reference recording"],[data.b,"Recording to examine"]]) {
      const n=el("div");
      const plural=(n,noun)=>n+" "+noun+(n===1?"":"s");
      n.append(el("h3","",title),el("p","",side.name),el("p","muted",plural(side.answered,"result")+" · "+plural(side.denies,"deny decision")+" · "+plural(side.failures,"tool error")));
      pair.append(n);
    }
    out.append(pair);
    // Differences are the substance of the screen, so they are visible; only the
    // matching detail (keys, hashes, long tails) stays behind a disclosure.
    const diffSections=el("div","compare-diffs");
    for(const[key,title]of [["only_in_a","Only in the reference recording"],["only_in_b","Only in the recording examined"],["changed","Same call key, a different recorded outcome or count"]]) {
      const rows=(data.diff[key]||[]);
      const n=el("section","compare-diff");
      const head=el("div","section-heading");
      head.append(el("h3","",title),el("span","muted",rows.length+(rows.length===1?" entry":" entries")));
      n.append(head);
      if(!rows.length) {
        n.append(el("p","muted","None."));
      } else {
        const list=el("ul","occurrence-list mono");
        for(const text of rows.slice(0,20))list.append(el("li","",String(text).slice(0,512)));
        n.append(list);
        if(rows.length>20)n.append(el("p","muted","Showing the first 20 of "+rows.length+"; open the matching detail for the rest."));
      }
      diffSections.append(n);
    }
    out.append(diffSections);
    out.append(el("p","muted",data.diff.order_differs?"The answered-call sequence differs. Added or missing calls also change this sequence.":"The answered-call sequence is unchanged."));
    const technical=el("details","comparison-technical");
    technical.append(el("summary","","Call matching detail and argument hashes"));
    const differences=el("ul","mono");
    for(const change of inv.changes)differences.append(el("li","",change));
    technical.append(differences);
    if(inv.change_count>100)technical.append(el("p","muted","Showing first 100 differences."));
    technical.append(el("p","muted",data.scope));
    for(const[key,title]of [["only_in_a","Only in this recording"],["only_in_b","Only in compared recording"],["changed","Same call key, different recorded outcomes / count"]]) {
      const n=el("div");
      n.append(el("h3","",title));
      const list=el("ul","mono");
      for(const text of (data.diff[key]||[]).slice(0,100))list.append(el("li","",String(text).slice(0,512)));
      if((data.diff[key]||[]).length>100)list.append(el("li","muted","Showing the first 100 entries."));
      if(!list.children.length)list.append(el("li","muted","None"));
      n.append(list);
      technical.append(n);
    }
    const history=data.policy_history;
    if(history) {
      const section=el("section","policy-history");
      section.append(el("h3","","Policy decision history"),el("p","muted",history.total+" matched call histories changed (verdict, rule or reason). Matching shares the same occurrence uncertainty."));
      for(const change of history.changes)section.append(el("p","mono",change));
      if(history.total>100)section.append(el("p","muted","Showing the first 100 changes."));
      out.append(section);
    }
    out.append(technical);
  } catch(e) {
    if(file===compareBaseFile()&&request===compareRequest)notice(out,e.message,"warning");
  } finally {
    if(file===compareBaseFile()&&request===compareRequest)$("run-compare").disabled=false;
  }
}
function clearTest() {
  $("lab-definition").hidden=true; labPreview=""; $("lab-save").disabled=true; $("lab-json").hidden=true; $("lab-json").textContent=""; $("lab-result").replaceChildren();
  ++testRequest;
  yamlPreview="";
  $("test-yaml").hidden=true;
  $("test-yaml").textContent="";
  $("save-test").disabled=true;
  $("test-result").replaceChildren();
}
function updateSelectedEvents() {
  $("sequence-actions").hidden=selectedEvents.length===0;
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

async function previewLab() {
  clearTest();
  if(!selectedCall){notice($("lab-result"),"Select a call in Activity first.","warning");return;}
  const kind=$("assertion-kind").value;if(!kind){notice($("lab-result"),"Choose an expectation above.","warning");return;}
  if(!$("lab-title-input").value.trim()){notice($("lab-result"),"Give this case a title.","warning");return;}
  const file=currentFile,seq=selectedCall.seq,request=++testRequest;
  try {
    const data=await api("/api/test",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({file,revision:detailRevision,seq,kind,fixture:$("fixture-path").value,seqs:selectedEvents.map(x=>x.seq),ceiling:kind==="attempt_ceiling"&&$("attempt-ceiling").value.trim()!==""?Number($("attempt-ceiling").value):null,run:true,lab:true,eligibility:"complete-boundary",title:$("lab-title-input").value,description:$("lab-description").value,reference:$("lab-reference").value,candidate:$("lab-candidate").value,conversation:($("lab-conversation")?.value||"").trim()})});
    if(request!==testRequest||file!==currentFile||seq!==selectedCall?.seq)return;
    labPreview=data.case_json;$("lab-definition").hidden=false;$("lab-json").textContent=labPreview;$("lab-json").hidden=false;$("lab-save").disabled=false;
    const out=$("lab-result"),result=data.result;notice(out,result.status+" · "+result.evidence,result.status==="FAIL"?"error":result.status==="INCONCLUSIVE"?"warning":"");
    for(const c of result.checks||[])out.append(el("p","",c.status+" · "+c.detail));
    out.append(el("p","muted",result.note));if(data.comparison_status!=="not_requested")out.append(el("p","muted","Reference comparison: "+data.comparison_status+". Open Behaviour for the complete recorded comparison; changes are not automatic failures."));
    out.append(button("Return to selected evidence",()=>{setTab("activity");selectCall(seq);}));
  }catch(e){if(request===testRequest&&file===currentFile)notice($("lab-result"),e.message,"warning");}
}
// saveLabCaseToLibrary stores the previewed case beside the recordings, where the
// library and the CLI both find it. It never overwrites: the server refuses an
// existing file unless the caller proves it read the current revision.
async function saveLabCaseToLibrary() {
  if(!labPreview)return;
  const out=$("lab-result");
  let doc;
  try {
    doc=JSON.parse(labPreview);
  } catch(e) {
    notice(out,"The case preview is not valid JSON. Preview it again.","warning");
    return;
  }
  const post=async replaces=>api("/api/lab/case", {
    method:"POST",headers: {
      "Content-Type":"application/json"
    }
    ,body:JSON.stringify( {
      file:"",case:doc,replaces
    }
    )
  }
  );
  try {
    const data=await post("");
    notice(out,"Saved as "+data.file+" in your regression library.","");
    out.append(button("Open saved case →",()=>{showLab();openLabCase(data.file);},"primary"));
  } catch(e) {
    if(!/already exists/.test(e.message)) {
      notice(out,e.message,"warning");
      return;
    }
    notice(out,e.message,"warning");
    const replace=button("Replace that case",async()=> {
      try {
        const list=await api("/api/lab");
        const existing=(list.cases||[]).find(c=>(c.title||"")===doc.title);
        if(!existing)throw new Error("That file is no longer in the library. Preview the case again.");
        const data=await post(existing.revision);
        notice(out,"Replaced "+data.file+" after reading the current file.","");
      } catch(err) {
        notice(out,err.message,"warning");
      }
    }
    ,"primary");
    const rename=button("Use a different title",()=> {
      notice(out,"Change the case title and preview again to save it under a different name.","");
      $("lab-title-input").focus();
    }
    );
    const row=el("div","actions");
    row.append(replace,rename);
    out.append(row);
  }
}
async function saveLabCase() {
 if(!labPreview)return;const text=labPreview;
 try {if(window.showSaveFilePicker){const handle=await window.showSaveFilePicker({suggestedName:"tapelog-case.json",types:[{description:"Tapelog regression case",accept:{"application/json":[".json"]}}]});const existing=await handle.getFile();if(existing.size>0)throw new Error("That file already contains data. Choose a new filename to preserve it.");const stream=await handle.createWritable();await stream.write(text);await stream.close();notice($("lab-result"),"Case saved. Recordings were not modified.");} else {const url=URL.createObjectURL(new Blob([text],{type:"application/json"}));const a=el("a");a.href=url;a.download="tapelog-case-"+Date.now()+".json";a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);notice($("lab-result"),"Case downloaded. Put the case alongside the referenced recordings before running it.");}}catch(e){if(e.name!=="AbortError")notice($("lab-result"),e.message,"warning");}
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
    out.append(el("p","","Use setup to preview a configuration for an existing MCP server, save a new candidate and review it before activation. Generated configs create a fresh recording on each start."),el("pre","","tapelog setup --client roo --config <client.json> --server <name>\ntapelog web --dir <recordings-directory>"),el("p","","Restart the wrapped server in your client, explicitly call one of its MCP tools, then refresh Sessions. Tapelog cannot see the client’s built-in file, terminal or browser operations."),el("p","","Use Activity to investigate policy decisions and failures. All events preserves file order. Compare uses recorded responses. Make a test creates a tool-level assertion without copying payloads."),el("p","","Policy Studio explains the rules you enforce, drafts a narrow rule and simulates it against a recording before you save anything. It never writes or activates a policy."));
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
  if(!$("behaviour-view").hidden)loadBehaviour();
  else if(!$("comparisons-view").hidden)runWorkspaceComparison();
  else if(!$("investigations-view").hidden)renderInvestigations();
  else if(!$("studio-view").hidden)loadStudioPolicies();
  else if(!$("lab-view").hidden)loadLab();
  else if(!$("session-view").hidden) {
    if(currentTab==="events")loadEvents();
    else if(currentTab==="repeats")loadRepeats();
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
for(const tab of document.querySelectorAll("[data-tab]")) {
  tab.onclick=()=>setTab(tab.dataset.tab);
  tab.onkeydown=e=> {
    const tabs=[...document.querySelectorAll("[data-tab]")];
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
$("lab-preview").onclick=previewLab;$("lab-save").onclick=saveLabCase;
for(const id of ["lab-title-input","lab-description","lab-reference","lab-candidate"])$(id).oninput=clearTest;
$("assertion-kind").onchange=()=>{
  clearTest(); const kind=$("assertion-kind").value; $("ceiling-field").hidden=kind!=="attempt_ceiling";
  $("lab-preview").disabled=kind==="never_answered";
  $("selected-events").hidden=kind!=="sequence";
  $("choose-events").hidden=kind!=="sequence";
  const descriptions={denied:"At least one call to this tool must have a final recorded policy decision of deny.",attempted:"At least one attempt must reach this recorded boundary.",answered:"At least one call to this tool must have a recorded result.",never_answered:"This rule is available in Advanced legacy YAML export. Choose a supported rule above to create a strict case.",no_attempts:"No attempts to this tool may appear at the recorded boundary.",sequence:"The selected answered tool names must appear in the displayed order, as a subsequence.",invariant:"Checks for recorded deny decisions with recorded results; it cannot observe actions outside this boundary.",attempt_ceiling:"Counts all recorded boundary attempts for the selected tool, including unanswered calls."};
  $("assertion-help").textContent=kind==="error_absent"?"The case uses a stable phrase from this recorded error. Preview shows the exact expectation for your review.":descriptions[kind]||"Choose a rule that reflects the behaviour you want to test.";
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
window.addEventListener("hashchange",restoreWorkspaceRoute);
window.addEventListener("beforeunload",event=> {
  if(suiteDraft&&suiteDraft.dirty) {
    event.preventDefault();
    event.returnValue="";
  }
}
);
window.addEventListener("popstate",restoreWorkspaceRoute);
loadLibrary().then(async()=>{await restoreWorkspaceRoute();refreshPending();});
setInterval(refreshPending,3000);

/* ------------------------------------------------------------------
   Policy Studio (M5). Read-only: every decision shown here comes from
   the server's policy engine, and nothing is written or activated from
   this page. Recorded text is inserted with textContent only.
   ------------------------------------------------------------------ */
const ACTION_MEANINGS= {
  allow:"The call proceeds without asking anyone. Limits, result scanning, redaction and schema checks still apply.",
  confirm:"The call is parked until a human chooses allow once, allow for the session, or deny. With no terminal and no auto-confirm the call is denied.",
  deny:"The call is refused and not forwarded. A recorded result on a denied call is still evidence; denial alone does not prove a tool never ran."
};
function policyActionLabel(action) {
  return {
    allow:"Allow",confirm:"Require approval",deny:"Block"
  }
  [action]||action||"Unknown";
}
function policyActionMeaning(action) {
  return ACTION_MEANINGS[action]||"";
}
function ruleChip(action) {
  return el("span","chip "+action,policyActionLabel(action));
}
function transitionText(c) {
  if(!c||!c.changed)return "No change";
  return policyActionLabel(c.recorded_verdict)+" → "+policyActionLabel(c.candidate_verdict);
}
function simulationCounts(simulation) {
  const s=simulation||{}
  ;
  return {
    total:s.total??0,changed:s.changed??0,same:s.same??0,denied:s.newly_denied??0,allowed:s.newly_allowed??0,confirm:s.newly_confirm??0
  };
}
function filterSimulated(simulation,filter) {
  const calls=(simulation&&simulation.calls)||[];
  switch(filter) {
    case"all":return calls;
    case"denied":return calls.filter(c=>c.changed&&c.candidate_verdict==="deny");
    case"allowed":return calls.filter(c=>c.changed&&c.candidate_verdict==="allow");
    case"confirm":return calls.filter(c=>c.changed&&c.candidate_verdict==="confirm");
    case"same":return calls.filter(c=>!c.changed);
    default:return calls.filter(c=>c.changed);
  }
}
function commaList(value) {
  return String(value||"").split(",").map(s=>s.trim()).filter(Boolean);
}
function expiresRFC3339(value) {
  if(!value)return "";
  const d=new Date(value);
  if(Number.isNaN(d.getTime()))throw new Error("That expiry date cannot be read. Pick a date and time, or clear the field.");
  return d.toISOString().replace(/\.\d{3}Z$/,"Z");
}
const studioState= {
  sources:[],selected:"",inspection:null,candidate:null,simulation:null,filter:"changed",request:0,previewRequest:0,simRequest:0,conditionSeq:0,draft:null
};
function showStudioStatus(text,kind="") {
  const box=$("studio-status");
  box.replaceChildren();
  if(text)notice(box,text,kind);
}
async function openStudio(preselect) {
  showView("studio");
  $("breadcrumb").textContent="Workspace / Policy Studio";
  clearError();
  if(typeof preselect==="string"&&preselect)studioState.selected=preselect;
  setWorkspaceRoute("#studio"+(studioState.selected?"="+encodeURIComponent(studioState.selected):""));
  await loadStudioPolicies();
  loadStudioSessions();
  if(!$("builder-action-help").textContent)$("builder-action-help").textContent=policyActionMeaning($("builder-action").value);
}
async function loadStudioPolicies() {
  const request=++studioState.request;
  showStudioStatus("Reading policies…");
  try {
    const data=await api("/api/policy/policies");
    if(request!==studioState.request)return;
    studioState.sources=(data.active?[data.active]:[]).concat(data.packs||[]);
    const notes=$("studio-notes");
    notes.replaceChildren();
    if(!data.active)notice(notes,"No policy file is configured for this workbench. Start it with tapelog web --policy <file> to explain the rules you enforce. The built-in packs below can still be read and extended.","warning");
    const base=$("builder-base"),previousBase=base.value;
    base.replaceChildren(new Option("New candidate file","new"));
    if(data.active)base.add(new Option("Extend: workspace policy (read-only copy)","active"));
    for(const p of data.packs||[])base.add(new Option("Extend pack: "+(p.title||p.name),"pack:"+p.name));
    if([...base.options].some(o=>o.value===previousBase))base.value=previousBase;
    renderStudioSources();
    const preferred=studioState.selected||(data.active?"active":((data.packs&&data.packs.length)?"pack:"+data.packs[0].name:""));
    if(preferred)await selectStudioSource(preferred);
    else {
      $("studio-rules").replaceChildren();
      empty($("studio-summary"),"No policy to show","This workbench was started without --policy, and no built-in pack was found.");
      showStudioStatus("");
    }
  } catch(e) {
    if(request===studioState.request) {
      showStudioStatus(e.message,"warning");
      error(e);
    }
  }
}
function renderStudioSources() {
  const list=$("studio-source-list");
  list.replaceChildren();
  if(!studioState.sources.length) {
    list.append(el("p","muted","No policies available."));
    return;
  }
  for(const src of studioState.sources) {
    const row=el("div","source-row");
    const pick=button("",()=>selectStudioSource(src.id),"source-item"+(studioState.selected===src.id?" selected":""));
    pick.setAttribute("aria-pressed",String(studioState.selected===src.id));
    const title=el("div");
    title.append(el("strong","",src.kind==="active"?"Workspace policy":(src.title||src.name)),el("small","",src.kind==="active"?(src.path||"active policy"):("built-in pack · "+src.name)));
    pick.append(title);
    if(src.error)pick.append(el("span","badge failed","Does not compile"));
    row.append(pick);
    const simulate=button("Simulate",()=>simulatePolicySource(src),"quiet source-simulate");
    simulate.setAttribute("aria-label","Simulate "+src.name+" against the selected recording");
    row.append(simulate);
    list.append(row);
  }
}
async function selectStudioSource(id) {
  const request=++studioState.request;
  studioState.selected=id;
  renderStudioSources();
  showStudioStatus("Reading "+(id==="active"?"the workspace policy":id.replace("pack:",""))+"…");
  try {
    const data=await api("/api/policy/inspect?source="+encodeURIComponent(id));
    if(request!==studioState.request)return;
    studioState.inspection=data;
    renderStudioInspection();
    showStudioStatus("");
  } catch(e) {
    if(request===studioState.request) {
      showStudioStatus(e.message,"warning");
      error(e);
    }
  }
}
function renderStudioInspection() {
  const insp=studioState.inspection;
  const rules=$("studio-rules"),findings=$("studio-findings"),source=$("studio-source");
  rules.replaceChildren();
  findings.replaceChildren();
  source.replaceChildren();
  if(!insp)return;
  // The main column is read on its own, so it repeats which source it is
  // explaining: the selected row lives in a different column of the page.
  const current=el("div","studio-current");
  const sourceHead=el("div","");
  sourceHead.append(el("div","eyebrow","EXPLAINING"),el("h2","",insp.name||insp.title||insp.id||"Policy source"));
  current.append(sourceHead);
  if(insp.path)current.append(el("p","studio-current-path",insp.path));
  findings.append(current);
  const analysis=insp.analysis||null;
  if(!insp.valid||!analysis) {
    stats($("studio-summary"),[[0,"Rules"],[0,"Command-flow rules"],[insp.bytes||0,"Bytes"]]);
    notice(findings,"This policy does not compile: "+String(insp.error||"unknown error")+". It cannot be explained or extended until the file is fixed.","error");
    addSourceBlock(source,insp);
    return;
  }
  const findingCount=(analysis.warnings||[]).length+(analysis.unsupported||[]).length;
  stats($("studio-summary"),[[analysis.rules.length,"Rules"],[analysis.flows.length,"Flow rules"],[analysis.broad_permissions,"Broad permissions"],[findingCount,findingCount===1?"Finding":"Findings"]]);
  const summary=el("p","lede");
  summary.textContent="Default when no rule matches: "+analysis.default_label+". "+analysis.default_effect;
  findings.append(summary);
  for(const warning of analysis.warnings||[])notice(findings,warning,"warning");
  if(!(analysis.warnings||[]).length) {
    notice(findings,"No structural findings: no broad permissions, no unreachable rules, no conditions that cannot match. This describes the rules, not the safety of your agent.");
  }
  if((analysis.unsupported||[]).length) {
    const box=el("details","unsupported");
    box.append(el("summary","","Fields the engine ignores ("+analysis.unsupported.length+")"));
    box.append(el("p","muted","These keys are present in the document but are not read by the policy engine. They are reported here rather than dropped."));
    for(const item of analysis.unsupported)box.append(el("p","mono",item));
    findings.append(box);
  }
  const heading=el("div","section-heading");
  heading.append(el("h2","","Rules and precedence"),el("span","muted","First match wins, top to bottom"));
  rules.append(heading);
  if(!analysis.rules.length) {
    const box=el("div");
    empty(box,"No rules","Every call gets the default decision above.");
    rules.append(box);
  }
  for(const rule of analysis.rules)rules.append(ruleCard(rule));
  if((analysis.flows||[]).length) {
    const flowHeading=el("div","section-heading");
    flowHeading.append(el("h2","","Command-flow rules"),el("span","muted","Evaluated before per-call rules"));
    rules.append(flowHeading);
    for(const flow of analysis.flows)rules.append(flowCard(flow));
  }
  const limits=el("div","section-heading");
  limits.append(el("h2","","Session limits"));
  rules.append(limits);
  const limitsBox=el("div","limits-card");
  if(!analysis.limits.enabled)limitsBox.append(el("p","muted","No session limits are configured in this policy."));
  for(const line of analysis.limits.summary||[])limitsBox.append(el("p","","· "+line));
  limitsBox.append(el("p","muted",analysis.limits.note));
  limitsBox.append(el("p","muted","Result injection scanning: "+analysis.injection.mode+". "+(analysis.injection.summary||"")));
  rules.append(limitsBox);
  addSourceBlock(source,insp);
}
function ruleCard(rule) {
  const card=el("details","rule-card"+(rule.broad?" broad":""));
  card.open=true;
  const head=el("summary","rule-head");
  head.append(el("span","rule-pos","#"+rule.position),ruleChip(rule.action),el("strong","mono",rule.id));
  card.append(head);
  card.append(el("p","rule-summary",rule.summary));
  if(rule.reason)card.append(el("p","muted","Recorded explanation: "+rule.reason));
  const facts=el("dl","rule-facts");
  for(const [term,value]of [["Tools",rule.tool_summary],["Scope",rule.scope_summary],["What the boundary does",rule.action_effect]]) {
    facts.append(el("dt","",term),el("dd","",value));
  }
  card.append(facts);
  if((rule.conditions||[]).length) {
    card.append(el("h3","","Argument conditions (all must hold)"));
    const list=el("ul","condition-list");
    for(const cond of rule.conditions)list.append(conditionItem(cond));
    card.append(list);
  }
  if(rule.broad) {
    const warn=el("div","rule-warning");
    warn.append(el("strong","","Broad permission. "));
    warn.append(el("span","",(rule.broad_why||[]).join("; ")+"."));
    card.append(warn);
  }
  if(rule.unreachable) {
    const warn=el("div","rule-warning");
    warn.append(el("strong","","Never reached. "));
    warn.append(el("span","",rule.unreachable));
    card.append(warn);
  }
  if((rule.where_raw||[]).length) {
    const raw=el("details","rule-raw");
    raw.append(el("summary","","Original Cedar condition"+(rule.where_raw.length>1?"s":"")));
    raw.append(el("p","muted","Shown exactly as the engine reads it. The explanations above never replace this text."));
    for(const expr of rule.where_raw)raw.append(el("pre","",expr));
    card.append(raw);
  }
  return card;
}
function conditionItem(cond) {
  const item=el("li","condition");
  if(cond.simplified)item.append(el("span","cond-summary",cond.summary));
  else item.append(el("span","cond-summary muted","Advanced condition: shown as source below, not simplified."));
  if(cond.note)item.append(el("span","cond-note",cond.note));
  if(cond.problem) {
    const problem=el("span","cond-problem","Cannot match as written: "+cond.problem);
    item.append(problem);
  }
  item.append(el("pre","cond-expr",cond.expr));
  return item;
}
function flowCard(flow) {
  const card=el("details","rule-card flow");
  card.open=true;
  const head=el("summary","rule-head");
  head.append(el("span","rule-pos","F"+flow.position),ruleChip(flow.action),el("strong","mono",flow.id));
  card.append(head);
  card.append(el("p","rule-summary",flow.summary));
  if(flow.reason)card.append(el("p","muted","Recorded explanation: "+flow.reason));
  const facts=el("dl","rule-facts");
  facts.append(el("dt","","Source tools"),el("dd","",(flow.from||[]).join(", ")));
  facts.append(el("dt","","Sink tools"),el("dd","",(flow.to||[]).join(", ")));
  facts.append(el("dt","","Mode"),el("dd","",flow.mode==="value"?"value-level taint (experimental)":"session taint (conservative)"));
  card.append(facts);
  return card;
}
function addSourceBlock(parent,insp) {
  const box=el("details","policy-source");
  const summary=el("summary","","Original policy source");
  box.append(summary);
  box.append(el("p","muted","This is the file exactly as the engine reads it. It is read-only here: the workbench cannot edit, format or rewrite it."));
  const meta=el("p","muted mono");
  meta.textContent=(insp.path||insp.name||"")+" · "+String(insp.bytes||0)+" bytes · sha256 "+String(insp.digest||"").slice(0,16)+"…";
  box.append(meta);
  box.append(el("pre","policy-yaml",String(insp.yaml||"")));
  if(insp.scope)box.append(el("p","muted",insp.scope));
  parent.append(box);
}
async function loadStudioSessions() {
  const select=$("builder-session");
  const previous=select.value||currentFile;
  try {
    const data=await api("/api/sessions?limit=60");
    select.replaceChildren(new Option("No recording selected",""));
    for(const s of data.sessions||[])select.add(new Option(s.name+(s.denies?" · "+s.denies+" deny":""),s.file));
    if([...select.options].some(o=>o.value===(pendingComparison?.b||previous)))select.value=pendingComparison?.b||previous;
  } catch(e) {
    select.replaceChildren(new Option("No recording selected",""));
  }
}
function addConditionRow(preset) {
  const row=el("div","cond-row");
  row.dataset.cid=String(++studioState.conditionSeq);
  const arg=el("input","cond-arg");
  arg.type="text";
  arg.placeholder="argument name";
  arg.setAttribute("autocomplete","off");
  arg.setAttribute("spellcheck","false");
  arg.setAttribute("aria-label","Argument name");
  if(preset&&preset.arg)arg.value=preset.arg;
  const kind=el("select","cond-kind");
  kind.setAttribute("aria-label","Value type");
  for(const [value,label]of [["string","text"],["number","number"],["bool","true/false"]])kind.add(new Option(label,value));
  if(preset&&preset.kind)kind.value=preset.kind;
  const op=el("select","cond-op");
  op.setAttribute("aria-label","Comparison");
  for(const [value,label]of [["equals","equals"],["starts_with","starts with"],["ends_with","ends with"],["contains","contains"]])op.add(new Option(label,value));
  if(preset&&preset.op)op.value=preset.op;
  const value=el("input","cond-value");
  value.type="text";
  value.placeholder="value";
  value.setAttribute("autocomplete","off");
  value.setAttribute("aria-label","Matched value");
  if(preset&&preset.value)value.value=preset.value;
  const remove=button("Remove",()=>row.remove(),"quiet");
  remove.setAttribute("aria-label","Remove this condition");
  row.append(arg,kind,op,value,remove);
  row.refs= {
    arg,kind,op,value
  };
  kind.onchange=()=> {
    const numeric=kind.value!=="string";
    op.replaceChildren();
    for(const [v,l]of(numeric?[["equals","equals"]]:[["equals","equals"],["starts_with","starts with"],["ends_with","ends with"],["contains","contains"]]))op.add(new Option(l,v));
    value.placeholder=numeric?(kind.value==="bool"?"true":"number"):"value";
  }
  ;
  $("builder-conditions").append(row);
  return row;
}
function readBuilderRule() {
  const tools=commaList($("builder-tools").value);
  if(!tools.length)throw new Error("Enter at least one tool name.");
  const conditions=[];
  for(const child of $("builder-conditions").children) {
    const refs=child.refs;
    if(!refs)continue;
    const arg=refs.arg.value.trim(),value=refs.value.value;
    if(!arg&&!value&&!refs.value.value.trim())continue;
    if(!arg)throw new Error("Every argument condition needs an argument name.");
    if(value==="")throw new Error("Enter a value for the "+arg+" condition.");
    conditions.push( {
      arg,kind:refs.kind.value,op:refs.op.value,value
    }
    );
  }
  return {
    id:$("builder-id").value.trim(),
    tools,
    action:$("builder-action").value,
    reason:$("builder-reason").value.trim(),
    tasks:commaList($("builder-tasks").value),
    expires:expiresRFC3339($("builder-expires").value),
    conditions
  };
}
function readBuilderLimits() {
  const read=(id,label)=> {
    const raw=$(id).value.trim();
    if(raw==="")return 0;
    if(!/^[0-9]+$/.test(raw))throw new Error(label+" must be a whole number of zero or more.");
    return Number(raw);
  }
  ;
  const limits= {
    max_calls:read("builder-max-calls","Max tool-call attempts"),
    max_per_minute:read("builder-max-per-minute","Max attempts per minute"),
    max_response_bytes:read("builder-max-bytes","Max tool-result bytes")
  };
  const set=limits.max_calls>0||limits.max_per_minute>0||limits.max_response_bytes>0;
  return set?limits:null;
}
function syncLimitsAvailability() {
  const isNew=($("builder-base").value||"new")==="new";
  const fieldset=$("builder-limits-fieldset");
  fieldset.disabled=!isNew;
  fieldset.classList.toggle("muted-fieldset",!isNew);
}
function clearStudioDraft() {
  $("builder-tools").value="";
  $("builder-reason").value="";
  $("builder-id").value="";
  $("builder-tasks").value="";
  $("builder-expires").value="";
  $("builder-max-calls").value="";
  $("builder-max-per-minute").value="";
  $("builder-max-bytes").value="";
  $("builder-conditions").replaceChildren();
  studioState.draft=null;
  $("builder-context").hidden=true;
  $("builder-context").replaceChildren();
  $("candidate-panel").hidden=true;
  $("simulation-panel").hidden=true;
  studioState.candidate=null;
  studioState.simulation=null;
  showStudioStatus("");
}
async function previewCandidate() {
  clearError();
  $("candidate-panel").hidden=true;
  $("simulation-panel").hidden=true;
  let rule,limits;
  try {
    rule=readBuilderRule();
    limits=($("builder-base").value||"new")==="new"?readBuilderLimits():null;
  } catch(e) {
    showStudioStatus(e.message,"warning");
    return;
  }
  const body= {
    base:$("builder-base").value||"new",default:$("builder-default").value,rule,task:""
  };
  if(limits)body.limits=limits;
  const file=$("builder-session").value;
  if(file) {
    body.file=file;
    if(file===currentFile&&currentInfo)body.revision=currentInfo.revision||"";
  }
  const request=++studioState.previewRequest;
  $("builder-preview").disabled=true;
  showStudioStatus("Validating and simulating…");
  try {
    const data=await api("/api/policy/preview", {
      method:"POST",headers: {
        "Content-Type":"application/json"
      }
      ,body:JSON.stringify(body)
    }
    );
    if(request!==studioState.previewRequest)return;
    studioState.candidate=data;
    renderCandidate(data);
    showStudioStatus("");
  } catch(e) {
    if(request===studioState.previewRequest) {
      showStudioStatus(e.message,"warning");
      error(e);
    }
  } finally {
    if(request===studioState.previewRequest)$("builder-preview").disabled=false;
  }
}
function renderCandidate(data) {
  const panel=$("candidate-panel");
  panel.hidden=false;
  const summary=$("candidate-summary");
  summary.replaceChildren();
  stats(summary,[[data.rule_id||"(generated)","Rule id"],[data.extended?"Inserted":"New document","Placement"],[data.analysis&&data.analysis.rules?data.analysis.rules.length:0,"Rules in candidate"],[data.valid?"Compiles":"Does not compile","Compiler"]]);
  const status=$("candidate-status");
  status.replaceChildren();
  if(!data.valid)notice(status,"The candidate does not compile: "+String((data.validation&&data.validation.error)||"unknown error"),"error");
  else notice(status,data.extended?"Validated. Review the changes below; saving is a separate, explicit step.":"Validated as a standalone candidate. It does not inherit rules from any other policy.","");
  const notes=$("candidate-notes");
  notes.replaceChildren();
  for(const note of data.notes||[])notes.append(el("p","muted",note));
  const warnings=$("candidate-warnings");
  warnings.replaceChildren();
  for(const warning of data.warnings||[])notice(warnings,warning,"warning");
  $("candidate-yaml").textContent=String(data.candidate||"");
  $("candidate-digest").textContent="sha256 "+String(data.digest||"");
  $("candidate-cli").textContent="tapelog policy test --policy <saved-file>.yaml --calls calls.jsonl\ntapelog policy whatif --policy <saved-file>.yaml <recording>.jsonl\n\ntapelog record --policy <saved-file>.yaml -- <server command>";
  $("candidate-save").disabled=!data.valid;
  renderSimulation(data);
  panel.scrollIntoView( {
    block:"nearest"
  }
  );
}
function renderSimulation(data) {
  const panel=$("simulation-panel");
  const sim=data&&data.simulation?data.simulation:null;
  studioState.simulation=sim;
  const status=$("simulation-status");
  status.replaceChildren();
  if(!sim) {
    panel.hidden=false;
    stats($("simulation-summary"),[[0,"Calls re-evaluated"]]);
    $("simulation-filters").replaceChildren();
    $("simulation-list").replaceChildren();
    $("simulation-notes").replaceChildren();
    if(data&&data.simulation_error)notice(status,"Simulation unavailable: "+data.simulation_error+" Choose a completed, verified recording to simulate against.","warning");
    else notice(status,"Choose a recording above to see what this candidate would change.","");
    return;
  }
  panel.hidden=false;
  const counts=simulationCounts(sim);
  notice(status,counts.changed?("Against this recording the candidate changes "+counts.changed+" of "+counts.total+" recorded decisions."):"Against this recording the candidate changes nothing.",counts.denied?"warning":"");
  stats($("simulation-summary"),[[counts.total,"Calls re-evaluated"],[counts.changed,"Decisions changed"],[counts.denied,"Newly blocked"],[counts.allowed,"Newly allowed"],[counts.confirm,"New approvals"]]);
  renderSimulationFilters(counts);
  renderSimulationList();
  const notes=$("simulation-notes");
  notes.replaceChildren();
  for(const warning of(data&&data.warnings)||[])notice(notes,warning,"warning");
  if(data&&data.simulation_scope)notes.append(el("p","muted",data.simulation_scope));
  if(data&&data.simulation_limits)notes.append(el("p","muted",data.simulation_limits));
}
function renderSimulationFilters(counts) {
  const filters=$("simulation-filters");
  filters.replaceChildren();
  const options=[["changed","Changes only",counts.changed],["denied","Newly blocked",counts.denied],["allowed","Newly allowed",counts.allowed],["confirm","New approvals",counts.confirm],["all","All calls",counts.total]];
  for(const [value,label,count]of options) {
    const b=button(label+" ("+count+")",()=>{
      studioState.filter=value;
      renderSimulationFilters(counts);
      renderSimulationList();
    }
    ,"filter"+(studioState.filter===value?" selected":""));
    b.setAttribute("aria-pressed",String(studioState.filter===value));
    filters.append(b);
  }
}
function renderSimulationList() {
  const list=$("simulation-list");
  list.replaceChildren();
  const sim=studioState.simulation;
  if(!sim)return;
  const calls=filterSimulated(sim,studioState.filter);
  if(!calls.length) {
    list.append(el("p","muted",studioState.filter==="changed"?"No recorded decision would change under this candidate.":"No calls match this filter."));
    return;
  }
  for(const call of calls.slice(0,100))list.append(simulationRow(call));
  if(calls.length>100)list.append(el("p","muted","Showing the first 100 calls."));
}
function simulationRow(call) {
  const row=el("div","sim-row"+(call.changed?" changed":""));
  const head=el("div","sim-head");
  head.append(el("span","seq","#"+call.order),el("strong","mono",call.tool),el("span","sim-transition",transitionText(call)));
  row.append(head);
  const facts=el("dl","rule-facts");
  facts.append(el("dt","","Recorded"),el("dd","",policyActionLabel(call.recorded_verdict)+(call.recorded_rule?" · rule "+call.recorded_rule:"")));
  facts.append(el("dt","","Candidate"),el("dd","",policyActionLabel(call.candidate_verdict)+(call.candidate_rule?" · rule "+call.candidate_rule:"")));
  row.append(facts);
  if(call.candidate_reason)row.append(el("p","muted",call.candidate_reason));
  if(!call.answered)row.append(el("p","muted","This call was recorded without a result."));
  return row;
}
async function simulatePolicySource(source) {
  clearError();
  const file=$("builder-session").value;
  if(!file) {
    showStudioStatus("Choose a recording to simulate against first.","warning");
    $("builder-session").focus();
    return;
  }
  const request=++studioState.simRequest;
  showStudioStatus("Simulating "+source.name+" against the recording…");
  try {
    const data=await api("/api/policy/simulate", {
      method:"POST",headers: {
        "Content-Type":"application/json"
      }
      ,body:JSON.stringify( {
        source:source.id,file
      }
      )
    }
    );
    if(request!==studioState.simRequest)return;
    $("candidate-panel").hidden=true;
    studioState.candidate=null;
    renderSimulation(data);
    showStudioStatus("");
    $("simulation-panel").scrollIntoView( {
      block:"nearest"
    }
    );
  } catch(e) {
    if(request===studioState.simRequest) {
      showStudioStatus(e.message,"warning");
      error(e);
    }
  }
}
async function saveCandidate() {
  const data=studioState.candidate;
  if(!data||!data.valid)return;
  const text=String(data.candidate||"");
  const status=$("candidate-status");
  try {
    if(window.showSaveFilePicker) {
      const handle=await window.showSaveFilePicker( {
        suggestedName:"tapelog-candidate.yaml",types:[ {
          description:"Tapelog policy candidate",accept: {
            "text/yaml":[".yaml"]
          }
        }
        ]
      }
      );
      const existing=await handle.getFile();
      if(existing.size>0)throw new Error("That file already contains data. Choose a new filename so the existing policy is preserved.");
      const stream=await handle.createWritable();
      await stream.write(text);
      await stream.close();
      notice(status,"Saved the candidate. It is not active: point tapelog record --policy at it when you are ready. The recording and any active policy were not modified.","");
    } else {
      const url=URL.createObjectURL(new Blob([text], {
        type:"text/yaml"
      }
      ));
      const link=el("a");
      link.href=url;
      link.download="tapelog-candidate.yaml";
      link.click();
      URL.revokeObjectURL(url);
      notice(status,"Downloaded a uniquely named candidate file. It is not active.","");
    }
  } catch(e) {
    if(e&&e.name==="AbortError")return;
    notice(status,e.message||String(e),"warning");
  }
}
async function copyCandidate() {
  const data=studioState.candidate;
  if(!data)return;
  const status=$("candidate-status");
  try {
    await navigator.clipboard.writeText(String(data.candidate||""));
    notice(status,"Candidate copied to the clipboard. It is still not active.","");
  } catch(e) {
    notice(status,"This browser did not allow clipboard access. Select the YAML above and copy it manually.","warning");
  }
}
// createPolicyProposal carries one investigated call into the builder. It only
// prepares a draft: nothing is validated, simulated or saved until the
// operator asks for it.
async function createPolicyProposal() {
  if(!selectedCall) {
    showStudioStatus("Select a call in Activity first.","warning");
    return;
  }
  const file=currentFile,seq=selectedCall.seq,revision=currentInfo?currentInfo.revision:"";
  const wasTab=currentTab;
  showView("studio");
  $("breadcrumb").textContent="Workspace / Policy Studio";
  clearError();
  clearStudioDraft();
  await loadStudioPolicies();
  await loadStudioSessions();
  const select=$("builder-session");
  if([...select.options].some(o=>o.value===file))select.value=file;
  showStudioStatus("Reading the recorded call…");
  try {
    const data=await api("/api/policy/from-event", {
      method:"POST",headers: {
        "Content-Type":"application/json"
      }
      ,body:JSON.stringify( {
        file,seq,revision
      }
      )
    }
    );
    studioState.draft=data;
    $("builder-tools").value=data.tool;
    $("builder-action").value=data.suggested_action||"confirm";
    $("builder-action-help").textContent=policyActionMeaning($("builder-action").value);
    $("builder-reason").value="recorded call #"+seq+" on "+data.tool;
    $("builder-id").value="";
    $("builder-base").value="new";
    $("builder-conditions").replaceChildren();
    const context=$("builder-context");
    context.hidden=false;
    context.replaceChildren();
    context.append(el("strong","","From recorded call #"+seq+" · "+data.tool+" · recorded decision: "+(data.recorded_verdict||"not recorded")));
    for(const note of data.notes||[])context.append(el("p","",note));
    if((data.arg_keys||[]).length) {
      const box=el("details","");
      box.append(el("summary","","Recorded argument names you may match on"));
      const list=el("ul","arg-keys");
      for(const key of data.arg_keys) {
        const item=el("li","");
        item.append(el("span","mono",key.key),el("span","muted",key.kind+(key.redacted?" · redacted":" · "+(key.value_preview||""))+(key.matchable?"":" · not matchable in the builder")));
        if(key.matchable) {
          const use=button("Use",()=>addConditionRow( {
            arg:key.key,kind:key.kind==="number"?"number":(key.kind==="bool"?"bool":"string"),op:"equals",value:""
          }
          ),"quiet");
          use.setAttribute("aria-label","Match on argument "+key.key);
          item.append(use);
        }
        list.append(item);
      }
      box.append(list);
      box.append(el("p","muted","Values are not copied for you. Type the value you mean: a recorded value may be redacted, and anything you enter is written into the candidate file."));
      context.append(box);
    }
    $("builder-advanced").open=false;
    showStudioStatus("Draft prepared from the recording. Choose the outcome, then preview."," ");
  } catch(e) {
    showStudioStatus(e.message,"warning");
    error(e);
    setTab(wasTab);
  }
}
$("studio-nav").onclick=openStudio;
$("builder-action").onchange=()=> {
  $("builder-action-help").textContent=policyActionMeaning($("builder-action").value);
};
$("builder-add-condition").onclick=()=>addConditionRow(null);
$("builder-base").onchange=syncLimitsAvailability;
$("builder-preview").onclick=previewCandidate;
$("builder-clear").onclick=clearStudioDraft;
$("candidate-save").onclick=saveCandidate;
$("candidate-copy").onclick=copyCandidate;
$("candidate-discard").onclick=clearStudioDraft;
$("builder-action-help").textContent=policyActionMeaning("deny");
syncLimitsAvailability();
$("builder-session").onchange=()=> {
  if(studioState.candidate)showStudioStatus("The recording choice changed. Preview again to simulate against the new recording.","warning");
};


/* ------------------------------------------------------------------
   Agent behaviour (M6).

   Everything below reads /api/behaviour, which is derived from recorded MCP
   events only. These functions never execute a tool, never write a file and
   never guess at agent reasoning: the API states what it observed, and the
   interpretation text beside each finding says what it cannot know.
   ------------------------------------------------------------------ */
let behaviourRequest=0,behaviourData=null,repeatsRequest=0,deltaRequest=0,pendingDelta=null;
const FINDING_LABELS= {
  possible_retry_loop:"Possible retry loop",
  repeated_failure:"Repeated failure",
  repeated_denial:"Repeated denial",
  repeated_activity:"Repeated activity",
  alternating_sequence:"Alternating sequence",
  repeating_block:"Repeating block",
  insufficient_evidence:"Insufficient evidence"
};
// Kinds that mean more than plain repetition. The API counts them separately and
// the interface must never merge them back into one number.
const ATTENTION_KINDS=["possible_retry_loop","repeated_failure","repeated_denial","alternating_sequence","repeating_block"];
function percent(rate) {
  if(rate===null||rate===undefined)return "—";
  const pct=rate*100;
  return (pct===0||pct===100?pct.toFixed(0):pct.toFixed(1))+"%";
}
function rateCell(rate,n,unit) {
  if(rate===null||rate===undefined)return "undefined · no "+unit;
  return percent(rate)+" of "+n+" "+unit;
}
function barClass(rate) {
  if(rate===null||rate===undefined)return "bar-fill w0";
  return "bar-fill w"+Math.max(0,Math.min(10,Math.round(rate*10)));
}
// rateValue formats one comparison metric. A proportion is a percentage; a
// per-minute rate is a plain number, because multiplying it by 100 would be
// meaningless.
function rateValue(r,value) {
  if(value===null||value===undefined)return "—";
  if(r.unit==="per_minute")return value.toFixed(1)+" calls/min";
  return percent(value);
}
function findingLabel(kind) {
  return FINDING_LABELS[kind]||kind;
}
function showBehaviour(updateHash=true) {
  ++behaviourRequest;
  ++repeatsRequest;
  showView("behaviour");
  $("breadcrumb").textContent="Workspace / Behaviour";
  clearError();
  if(updateHash)setWorkspaceRoute("#behaviour");
  pendingDelta=behaviourLink();
  // The conversation panel is driven by the route, so it is applied after the
  // overview has rendered: a deep link then has conversation ids to show in its
  // comparison picker without a second round trip.
  loadBehaviour().then(()=>applyConversationRoute());
}
// behaviourLink reads #behaviour&a=<reference>&b=<recording>, so a comparison
// can be bookmarked or sent to a colleague without re-picking both recordings.
function behaviourLink() {
  if(!location.hash.startsWith("#behaviour"))return null;
  const cut=location.hash.indexOf("&");
  if(cut<0)return null;
  const query=new URLSearchParams(location.hash.slice(cut+1));
  if(!query.get("a")||!query.get("b"))return null;
  return {
    a:query.get("a"),b:query.get("b")
  };
}
async function loadBehaviour() {
  const request=++behaviourRequest;
  const days=$("behaviour-window").value;
  let from=$("behaviour-from").value,to=$("behaviour-to").value;
  if(days) {
    const end=new Date(),start=new Date(end.getTime()-(Number(days)-1)*86400000);
    from=start.toISOString().slice(0,10);
    to=end.toISOString().slice(0,10);
    $("behaviour-from").value=from;
    $("behaviour-to").value=to;
  }
  try {
    const data=await api("/api/behaviour?"+params( {
      q:$("behaviour-search").value,server:$("behaviour-server").value,from,to
    }
    ));
    if(request!==behaviourRequest)return;
    behaviourData=data;
    renderBehaviour(data);
    // The filters changed what "this view" means, so an open conversation must
    // be re-read against them: showing numbers from a recording the filters now
    // exclude is the same class of mistake as hiding the unattributed
    // remainder. If it is excluded, the panel says so instead of going stale.
    if(conversationView)await openConversation(conversationView.id,conversationView.compare, {
      updateHash:false,focus:false,offset:conversationView.offset,limit:conversationView.limit
    }
    );
  } catch(e) {
    if(request===behaviourRequest)error(e);
  }
}
function renderBehaviour(ov) {
  const notes=$("behaviour-notes");
  notes.replaceChildren();
  if(ov.library.analysed===0) {
    empty(notes,"No recordings to analyse","Point tapelog web --dir at a directory of session logs, then record a session with an MCP client. Behaviour findings come only from files Tapelog already wrote.");
  }
  for(const note of ov.notes||[])notes.append(el("p","behaviour-note",note));
  const c=ov.counts;
  stats($("behaviour-stats"),[[c.sessions,"Recordings in view"],[c.calls,"Tool calls"],[c.results,"Recorded results"],[c.failures,"Tool errors"],[c.denies,"Calls denied"],[c.unanswered,"Unanswered"]]);
  $("behaviour-coverage").textContent=coverageText(ov);
  renderBehaviourConversations(ov);
  renderBehaviourFailures(ov);
  renderBehaviourRepeats(ov);
  renderBehaviourTools(ov);
  renderBehaviourDenied(ov);
  renderBehaviourTrend(ov);
  renderBehaviourDefinitions(ov);
  renderDeltaPickers(ov);
  if(pendingDelta) {
    const wanted=pendingDelta;
    pendingDelta=null;
    const has=file=>[...$("delta-reference").options].some(o=>o.value===file);
    if(has(wanted.a)&&has(wanted.b)) {
      $("delta-reference").value=wanted.a;
      $("delta-current").value=wanted.b;
      runDelta().then(()=>$("delta-result").scrollIntoView( {
        block:"start"
      }
      ));
    } else {
      notice($("delta-result"),"That comparison link refers to a recording that is not in this library view any more. Choose the two recordings again.","warning");
    }
  }
}
function coverageText(ov) {
  const lib=ov.library;
  const parts=[];
  parts.push("Analysed "+lib.analysed+" of "+lib.files+" recordings");
  if(lib.matched!==lib.analysed)parts.push(lib.matched+" matched the current filters");
  if(lib.cached)parts.push(lib.cached+" reused from the analysis cache");
  if(lib.undated)parts.push(lib.undated+" have no usable timestamp");
  if(lib.truncated)parts.push("the "+lib.budget_files+"-recording limit was reached");
  const window=ov.window.from||ov.window.to?(ov.window.from||"any time")+" to "+(ov.window.to||"now"):"all recorded time";
  return parts.join(" · ")+". Window: "+window+".";
}
// Conversations are client-declared groups of activity inside one MCP session.
// The server decides membership, counts and ordering; this function only
// presents them, and says plainly which parts are a claim and which are not.
function renderBehaviourConversations(ov) {
  const box=$("behaviour-conversations"),recur=$("behaviour-recurrence");
  box.replaceChildren();
  recur.replaceChildren();
  const groups=ov.conversations||[];
  if(!groups.length) {
    empty(box,"No conversations to show","These recordings contain no tool calls, so there is nothing to attribute.");
    return;
  }
  const attributed=groups.filter(g=>g.attributed);
  const loose=groups.filter(g=>!g.attributed);
  const looseCalls=loose.reduce((n,g)=>n+g.calls,0);
  let separated=false;
  const caption=el("div","list-caption");
  caption.append(el("span","","OBSERVED GROUPS"),el("span","",attributed.length+(attributed.length===1?" conversation":" conversations")+(looseCalls?" · "+looseCalls+(looseCalls===1?" unattributed call":" unattributed calls"):"")));
  box.append(caption);
  // The unattributed remainder must ALWAYS render. It sorts last, so a plain
  // slice(0,25) dropped it as soon as a library held more than 25 groups —
  // hiding exactly the activity that could not be placed.
  const attributedSlice=groups.filter(g=>g.attributed).slice(0,25);
  const remainder=groups.filter(g=>!g.attributed);
  const visible=attributedSlice.concat(remainder);
  if(groups.length>visible.length)box.append(el("p","muted","Showing the first "+visible.length+" of "+groups.length+" groups; the unattributed remainder is always shown."));
  for(const g of visible) {
    if(!g.attributed&&!separated&&attributed.length) {
      const sep=el("div","list-caption unattributed-separator");
      sep.append(el("span","","NO DECLARED CONVERSATION"));
      box.append(sep);
      separated=true;
    }
    box.append(conversationItem(g));
  }

  renderRecurrence(recur,ov.recurrence||[],looseCalls);
}
function conversationItem(g) {
  const entry=el("div","conversation-entry");
  const item=el("details",g.attributed?"finding conversation":"finding conversation unattributed");
  const summary=el("summary","finding-row");
  const name=el("span","conversation-name");
  if(g.attributed) {
    const id=el("code","conversation-id",g.id);
    // The row truncates a long client-declared id; keep the whole value
    // available without widening the row.
    id.setAttribute("title",g.id);
    name.append(id);
    name.append(el("span","badge declared","client-declared"));
  } else {
    name.append(el("span","conversation-id unattributed-label","No declared conversation"));
  }
  const counts=el("span","conversation-counts");
  counts.append(el("span","badge",g.calls+(g.calls===1?" call":" calls")));
  if(g.denials)counts.append(el("span","badge denied",g.denials+" denied"));
  if(g.failures)counts.append(el("span","badge failed",g.failures+(g.failures===1?" error":" errors")));
  if(g.ambiguous_calls)counts.append(el("span","badge ambiguous",g.ambiguous_calls+(g.ambiguous_calls===1?" ambiguous call":" ambiguous calls")));
  summary.append(name,counts);
  // Keep the direct action visible, but outside the summary: nesting a button
  // inside a disclosure creates competing interactive controls for assistive tech.
  if(g.attributed) {
    const open=button("Open →",ev=> {
      if(ev) {
        ev.preventDefault();
        ev.stopPropagation();
      }
      openConversation(g.id);
    }
    ,"quiet");
    open.setAttribute("aria-label","Open conversation "+g.id);
    entry.append(open);
  }
  item.append(summary);

  const body=el("div","finding-body");
  if(!g.attributed) {
    body.append(el("p","criteria","These calls declared no conversation id, or declared one Tapelog rejects. They are shown together because they cannot be placed — not because they belong to one conversation."));
  } else {
    body.append(el("p","criteria","Grouped by the conversation id each call declared. The client chose that id: the recording does not prove who the client was, and nothing here says the conversation finished."));
  }
  const res=g.calls-g.results;
  const facts=el("p","muted");
  const fileCount=(g.files||[]).length;
  facts.textContent=g.calls+(g.calls===1?" call":" calls")+" · "+g.results+" with a recorded result"+(res>0?" · "+res+" with no recorded result — a denial, or an answer the recording does not show. Neither a success nor a failure":"")+(fileCount>1?" · across "+fileCount+" recordings":"");
  body.append(facts);
  if((g.tools||[]).length)body.append(el("p","muted","Tools: "+g.tools.join(", ")+"."));
  if((g.repeats||[]).length) {
    for(const r of g.repeats.slice(0,5))body.append(el("p","muted","Inside this conversation, "+repeatSentence(r)));
  }
  if(g.ambiguous_calls) {
    body.append(el("p","text-warn","Some calls share a request id with another outstanding call, so which response answers which is unknown. Those results are counted as ambiguous calls — unknown, not zero — and never as a success or a failure."));
  }
  const entries=g.timeline||[];
  if(entries.length) {
    const list=el("ol","occurrence-list timeline");
    const multiFile=(g.files||[]).length>1;
    for(const e of entries)list.append(timelineItem(e,multiFile));
    body.append(list);
    if(g.timeline_truncated)body.append(el("p","muted","Showing the first "+entries.length+" calls in this conversation; the recording holds more."));
  }
  item.append(body);
  entry.prepend(item);
  return entry;
}
function timelineItem(e,showFile) {
  const li=el("li","timeline-item"+(e.ambiguous?" ambiguous":""));
  const bits=[(e.tool||"call")];
  if(e.verdict==="deny")bits.push("denied");
  else if(e.verdict)bits.push(e.verdict);
  if(e.is_error)bits.push("tool error");
  else if(e.ambiguous)bits.push("result owner unknown");
  else if(!e.answered)bits.push("no recorded result");
  else bits.push("answered");
  const label=(showFile&&e.file?e.file+" · ":"")+bits.join(" · ")+(e.ts?" · "+when(e.ts):"");
  // The sequence number belongs in the gutter the timeline rail is drawn for;
  // appending the sentence as the only child put it inside that 52px column.
  li.append(el("span","seq","#"+e.seq));
  li.append(button(label,()=>openBehaviourCall(e.file,e.seq,e.index||0),"timeline-link"));
  return li;
}
function repeatSentence(r) {
  const what=r.tool?r.tool:"this operation";
  const n=r.count||r.occurrences||0;
  // "insufficient_evidence" is the detector's own name for "repeated, but not
  // enough to characterise". Print that meaning rather than the internal code.
  if(r.kind==="insufficient_evidence")return what+" repeated "+n+" times — a repetition was observed, but too few to characterise as a loop.";
  if(r.kind==="possible_retry_loop")return what+" repeated "+n+" times (possible retry loop).";
  return what+" repeated "+n+" times"+(r.kind?" ("+r.kind.replace(/_/g," ")+")":"")+".";
}
function renderRecurrence(box,recurrence,looseCalls) {
  if(!(recurrence||[]).length) {
    if(looseCalls)box.append(el("p","muted","No operation was seen in two different conversations. "+looseCalls+" call(s) could not be attributed, so recurrence across them cannot be claimed either way."));
    return;
  }
  box.append(el("h3","","Seen across conversations"));
  box.append(el("p","muted","The same operation appearing in more than one conversation. This is not a loop inside any one of them."));
  const list=el("ul","occurrence-list");
  for(const r of recurrence.slice(0,15)) {
    const li=el("li");
    li.append(el("span","tool-name",r.tool||r.fingerprint||"operation"),el("span","muted"," in "+r.conversations+" conversations ("+r.calls+" calls)"));
    list.append(li);
  }
  box.append(list);
}
function renderBehaviourFailures(ov) {
  const box=$("behaviour-failures");
  box.replaceChildren();
  if(!(ov.failures||[]).length) {
    box.append(el("p","muted",ov.counts.results?"No recorded tool errors in these recordings.":"No recorded results, so there is nothing to group yet."));
    return;
  }
  const caption=el("div","list-caption");
  caption.append(el("span","","FAILURE GROUP"),el("span","",ov.counts.signature_groups+" group(s)"));
  box.append(caption);
  for(const f of ov.failures.slice(0,25)) {
    const item=el("details","finding");
    const summary=el("summary","finding-row");
    const text=el("div");
    text.append(el("span","finding-label",f.label),el("span","tool-name",f.tool));
    summary.append(text,el("span","badge failed",f.occurrences+(f.occurrences===1?" occurrence":" occurrences")),el("span","finding-when",f.sessions+(f.sessions===1?" session":" sessions")));
    item.append(summary);
    const body=el("div","finding-body");
    body.append(el("p","criteria","Grouped because: "+f.criteria+"."));
    if(f.code)body.append(el("p","muted","Recorded error code: "+f.code));
    body.append(el("p","muted","First "+when(f.first_ts)+" · most recent "+when(f.last_ts)));
    if(f.fragment) {
      const p=el("p","muted","Stable phrase available for a regression test: ");
      p.append(el("code","",f.fragment));
      body.append(p);
      body.append(button("Draft a regression test for this error →",()=>draftAbsenceTest(f.recent[0])));
    } else {
      body.append(el("p","muted","This message has no phrase that stays the same between runs, so Tapelog will not invent one for a test."));
    }
    const list=el("ul","occurrence-list");
    for(const o of f.recent)list.append(occurrenceItem(o));
    body.append(list);
    if(f.occurrences>f.recent.length) {
      body.append(button("Show every occurrence ("+f.occurrences+") →",()=>loadSignature(f,body)));
    }
    item.append(body);
    box.append(item);
  }
  if(ov.failures.length>25)box.append(el("p","muted","Showing the 25 most frequent groups of "+ov.failures.length+"."));
}
function occurrenceItem(o) {
  const li=el("li");
  li.append(button(o.file+" · event #"+o.seq+(o.ts?" · "+when(o.ts):" · time not recorded"),()=>openBehaviourCall(o.file,o.seq,o.index),"occurrence-link"));
  return li;
}
// draftAbsenceTest is the Phase 7 loop: a recurring failure becomes a real
// engine assertion in three clicks, using the recording as the fixture.
async function draftAbsenceTest(o) {
  await openBehaviourCall(o.file,o.seq,o.index);
  if(!selectedCall) {
    error(new Error("Open one of the calls from this group, then choose the regression rule there."));
    return;
  }
  $("assertion-kind").value="error_absent";
  $("ceiling-field").hidden=true;
  clearTest();
  $("fixture-path").value=currentFile;
  setTab("test");
  createCaseFromCall();
}

async function loadSignature(group,body) {
  try {
    const data=await api("/api/behaviour/signature?id="+encodeURIComponent(group.id)+"&q="+encodeURIComponent($("behaviour-search").value)+"&server="+encodeURIComponent($("behaviour-server").value));
    const list=el("ul","occurrence-list");
    for(const o of data.occurrences)list.append(occurrenceItem(o));
    if(data.truncated)list.append(el("li","muted","Showing the first "+data.occurrences.length+" of "+data.total+"."));
    body.append(list);
    body.append(el("p","muted","Across "+data.sessions.length+" recordings. Matching rule: "+data.signature.criteria+"."));
  } catch(e) {
    body.append(el("p","text-warn",e.message));
  }
}
function renderBehaviourRepeats(ov) {
  const box=$("behaviour-repeats");
  box.replaceChildren();
  const repeats=ov.repeats||[];
  if(!repeats.length) {
    box.append(el("p","muted",ov.counts.calls<3?"Not enough calls in these recordings to look for a pattern.":"No repeated activity found. Tapelog looks for three or more identical calls, three or more repetitions of a short sequence, or three or more identical denials inside one recording."));
    return;
  }
  const attention=repeats.filter(r=>ATTENTION_KINDS.indexOf(r.kind)>=0);
  const insufficient=repeats.filter(r=>r.kind==="insufficient_evidence");
  const observed=repeats.filter(r=>ATTENTION_KINDS.indexOf(r.kind)<0&&r.kind!=="insufficient_evidence");
  if(attention.length) {
    box.append(el("h3","","Worth a look ("+attention.length+")"));
    for(const r of attention)box.append(repeatRow(r));
  }
  if(observed.length) {
    box.append(el("h3","","Observed, not necessarily wrong ("+observed.length+")"));
    for(const r of observed)box.append(repeatRow(r));
  }
  if(insufficient.length) {
    box.append(el("h3","","Not enough evidence to say ("+insufficient.length+")"));
    for(const r of insufficient)box.append(repeatRow(r));
  }
  box.append(el("p","muted","Repeat findings are observations about one recording. Tapelog does not know whether a repeat was necessary — open the recording to read the calls."));
}
function repeatRow(r) {
  const row=el("div","repeat-row");
  const head=el("div","repeat-head");
  head.append(el("span","kind "+r.kind,findingLabel(r.kind)));
  if(r.tool)head.append(el("span","tool-name",r.tool));
  head.append(el("span","badge",r.findings+(r.findings===1?" finding":" findings")));
  head.append(el("span","muted",r.occurrences+" calls · "+r.sessions+(r.sessions===1?" recording":" recordings")));
  row.append(head);
  if(r.sample)row.append(button("Open the first call →",()=>openBehaviourCall(r.sample.file,r.sample.seq,r.sample.index)));
  return row;
}
function renderBehaviourTools(ov) {
  const box=$("behaviour-tools");
  box.replaceChildren();
  if(!(ov.tools||[]).length) {
    box.append(el("p","muted","No tool calls recorded."));
    return;
  }
  const table=el("table","data-table"),thead=el("thead"),tbody=el("tbody");
  const hr=el("tr");
  for(const h of ["Tool","Calls","Results","Errors","Denied"])hr.append(el("th","",h));
  thead.append(hr);table.append(thead);
  for(const t of ov.tools.slice(0,12)) {
    const tr=el("tr");
    tr.append(el("td","",t.tool),el("td","num",t.calls),el("td","num",t.results),el("td","num",t.failures),el("td","num",t.denies));
    tbody.append(tr);
  }
  table.append(tbody);
  box.append(table);
  if(ov.tools.length>12)box.append(el("p","muted","Showing the 12 most used tools of "+ov.tools.length+"."));
}
function renderBehaviourDenied(ov) {
  const box=$("behaviour-denied");
  box.replaceChildren();
  if(!(ov.denied||[]).length) {
    box.append(el("p","muted","No deny decisions in these recordings."));
    return;
  }
  const table=el("table","data-table"),thead=el("thead"),tbody=el("tbody");
  const hr=el("tr");
  for(const h of ["Tool","Rule","Calls","Recordings"])hr.append(el("th","",h));
  thead.append(hr);table.append(thead);
  for(const d of ov.denied.slice(0,12)) {
    const tr=el("tr");
    tr.append(el("td","",d.tool),el("td","mono",d.rule||"rule not recorded"),el("td","num",d.calls),el("td","num",d.sessions));
    tbody.append(tr);
  }
  table.append(tbody);
  box.append(table);
  box.append(el("p","muted","A deny is a policy decision. One call can carry several decisions, so this column is not a call count."));
}
function renderBehaviourTrend(ov) {
  const box=$("behaviour-trend");
  box.replaceChildren();
  if(!(ov.trend||[]).length) {
    box.append(el("p","muted","Nothing to trend yet."));
    return;
  }
  const table=el("table","data-table trend-table"),thead=el("thead"),tbody=el("tbody");
  const hr=el("tr");
  for(const h of ["Recording","Calls","Results","Error rate","Deny rate","Repeats","Duration"])hr.append(el("th","",h));
  thead.append(hr);table.append(thead);
  const maxCalls=Math.max.apply(null,ov.trend.map(r=>r.calls).concat([1]));
  for(const r of ov.trend) {
    const tr=el("tr");
    const name=el("td");
    name.append(button(r.name||r.file,()=>openBehaviourCall(r.file,0,0),"link-button"));
    name.append(el("div","muted",when(r.first_ts)));
    const calls=el("td","num");
    calls.append(el("span","",r.calls));
    const bar=el("div","bar-track"),fill=el("div",barClass(r.calls/maxCalls));
    bar.append(fill);calls.append(bar);
    tr.append(name,calls,el("td","num",r.results),el("td","",rateCell(r.error_rate,r.results,"results")),el("td","",rateCell(r.denial_rate,r.calls,"calls")),el("td","num",r.repeat_findings?r.repeat_attention+" of "+r.repeat_findings:"none"),el("td","",fmtDuration(r.duration_ms)));
    tbody.append(tr);
  }
  table.append(tbody);
  box.append(table);
  box.append(el("p","muted","A recording with no recorded result has an undefined error rate, not a zero one. Call bars are scaled to the busiest recording in view ("+maxCalls+" calls), so they compare recordings, not time."));
}
function renderBehaviourDefinitions(ov) {
  const box=$("behaviour-definitions");
  box.replaceChildren();
  for(const d of ov.definitions||[]) {
    const row=el("div","definition-row");
    row.append(el("dt","",d.term),el("dd","",d.meaning));
    box.append(row);
  }
}
function renderDeltaPickers(ov) {
  const reference=$("delta-reference"),current=$("delta-current");
  const previous=[reference.value,current.value];
  reference.replaceChildren(new Option("Choose a reference recording",""));
  current.replaceChildren(new Option("Choose the recording to examine",""));
  for(const r of ov.trend||[]) {
    const label=(r.name||r.file)+" · "+r.calls+" calls · "+when(r.first_ts);
    reference.add(new Option(label,r.file));
    current.add(new Option(label,r.file));
  }
  if([...reference.options].some(o=>o.value===previous[0]))reference.value=previous[0];
  if([...current.options].some(o=>o.value===previous[1]))current.value=previous[1];
  $("run-delta").disabled=!(ov.trend||[]).length;
}
async function runDelta() {
  const reference=$("delta-reference").value,current=$("delta-current").value;
  const out=$("delta-result");
  out.replaceChildren();
  if(!reference||!current) {
    out.append(el("p","text-warn","Choose both a reference and a recording to examine."));
    return;
  }
  if(reference===current) {
    out.append(el("p","text-warn","Choose two different recordings."));
    return;
  }
  const request=++deltaRequest;
  $("run-delta").disabled=true;
  try {
    const data=await api("/api/behaviour/delta?"+params( {a:reference,b:current}));
    if(request!==deltaRequest)return;
    renderDelta(out,data);
  } catch(e) {
    if(request===deltaRequest)out.append(el("p","text-warn",e.message));
  } finally {
    if(request===deltaRequest)$("run-delta").disabled=false;
  }
}
function renderDelta(out,d) {
  out.append(el("p","",d.reference.name+" (reference) → "+d.current.name));
  const counts=el("div","stat-strip");
  stats(counts,[[d.current.calls,"Calls in this recording"],[d.new_failures.length,"New failure groups"],[d.new_denials.length,"New denial rules"],[d.new_repeats.length,"New repeat patterns"]]);
  out.append(counts);
  const sections=[
    ["New failure groups",d.new_failures,f=>f.label+" · "+f.tool+" · "+f.current+" in this recording"],
    ["Failure groups only in the reference",d.resolved_failures,f=>f.label+" · "+f.tool+" · "+f.reference+" in the reference"],
    ["Denials only in this recording",d.new_denials,f=>f.tool+" · rule "+(f.rule||"not recorded")+" · "+f.calls+" call(s)"],
    ["Denials only in the reference",d.resolved_denials,f=>f.tool+" · rule "+(f.rule||"not recorded")+" · "+f.calls+" call(s)"],
    ["Repeat patterns only in this recording",d.new_repeats,f=>findingLabel(f.kind)+" · "+(f.tool||"")+" · "+f.current+" finding(s)"],
    ["Repeat patterns only in the reference",d.resolved_repeats,f=>findingLabel(f.kind)+" · "+(f.tool||"")+" · "+f.reference+" finding(s)"]
  ];
  for(const [title,items,describe] of sections) {
    const box=el("div","delta-section");
    box.append(el("h3","",title+" ("+items.length+")"));
    if(!items.length)box.append(el("p","muted","None."));
    else {
      const list=el("ul","delta-list");
      for(const item of items)list.append(el("li","",describe(item)));
      box.append(list);
    }
    out.append(box);
  }
  const rateTable=el("table","data-table"),thead=el("thead"),tbody=el("tbody");
  const hr=el("tr");
  for(const h of ["Metric","Reference","This recording","Basis"])hr.append(el("th","",h));
  thead.append(hr);rateTable.append(thead);
  for(const r of d.rates) {
    const tr=el("tr");
    const basis="Denominators: "+(r.reference_n||0)+" / "+(r.current_n||0);
    tr.append(el("td","",r.metric),el("td","num",rateValue(r,r.reference)),el("td","num",rateValue(r,r.current)),el("td","muted",r.note?r.note+" "+basis:basis));
    tbody.append(tr);
  }
  rateTable.append(tbody);
  out.append(rateTable);
  const caveats=el("details","delta-caveats");
  caveats.append(el("summary","","What this comparison does not tell you"));
  for(const c of d.caveats)caveats.append(el("p","muted",c));
  caveats.append(el("p","muted",(d.scope||"").replace("session Compare tab","Recorded calls & decisions section above")));
  out.append(caveats);
  out.append(button("Open this recording →",()=>openBehaviourCall(d.current.file,0,0)));
}
async function openBehaviourCall(file,seq,index) {
  await openSession(file,true);
  if(!seq)return;
  $("activity-search").value="";
  $("activity-status").value="";
  $("activity-server").value="";
  if(typeof index==="number"&&index>=0)callOffset=Math.floor(index/50)*50;
  setTab("activity");
  await loadActivity();
  await selectCall(seq);
  $("inspector").scrollIntoView( {
    block:"nearest"
  }
  );
}

/* --- Session-level repeat findings ---------------------------------- */
async function loadRepeats() {
  const request=++repeatsRequest,file=currentFile;
  const status=$("repeats-status"),list=$("repeats-list");
  status.textContent="Running the detectors…";
  list.replaceChildren();
  try {
    const data=await api("/api/behaviour/repeats?"+params( {file}));
    if(request!==repeatsRequest||file!==currentFile)return;
    status.textContent="";
    if(!data.findings.length)empty(list,"No repetition found","Tapelog reports three or more identical calls, three or more repetitions of a short sequence, or three or more identical denials inside one recording. This recording has none of those shapes.");
    else for(const f of data.findings)list.append(findingCard(f,data));
    $("repeats-scope").textContent=data.scope;
    for(const note of data.notes||[])notice(list,note,"warning");
  } catch(e) {
    if(request===repeatsRequest&&file===currentFile)status.textContent=e.message;
  }
}
function findingCard(f,data) {
  const card=el("div","finding-card finding-"+f.kind);
  const head=el("div","finding-head");
  head.append(el("span","kind "+f.kind,findingLabel(f.kind)));
  if(f.tool)head.append(el("span","tool-name",f.tool));
  head.append(el("span","badge",f.count+(f.count===1?" call":" calls")));
  if(f.confidence==="possible")head.append(el("span","badge confirmation","interpretation"));
  if(f.confidence==="insufficient")head.append(el("span","badge unanswered","insufficient evidence"));
  card.append(head);
  card.append(el("p","",f.evidence));
  card.append(el("p","explanation",f.interpretation));
  if(f.identical_results)card.append(el("p","muted","Every recorded result for these calls is byte-identical."));
  if(f.median_gap_ms!==null&&f.median_gap_ms!==undefined)card.append(el("p","muted","Median gap between these calls: "+fmtDuration(f.median_gap_ms)+"."));
  else card.append(el("p","muted","Timestamps are missing for these calls, so their cadence cannot be assessed."));
  if(f.signature&&data.signatures&&data.signatures[f.signature])card.append(el("p","muted","Recorded error: "+data.signatures[f.signature]));
  const calls=el("div","finding-calls");
  calls.append(el("span","muted","Calls: "));
  for(const seq of f.seqs.slice(0,12))calls.append(button("#"+seq,()=>jumpToRepeatCall(f,seq),"chip"));
  if(f.seqs.length>12)calls.append(el("span","muted","+"+(f.seqs.length-12)+" more"));
  card.append(calls);
  if(singleToolName(f.tool))card.append(button("Make a test for this tool →",()=> {
    $("fixture-path").value=currentFile;
    setTab("test");
    $("tab-test").focus();
  }
  ));
  return card;
}
function singleToolName(tool) {
  return !!tool&&tool.indexOf("→")<0;
}
function jumpToRepeatCall(f,seq) {
  $("activity-search").value="";
  $("activity-status").value="";
  $("activity-server").value="";
  callOffset=Math.max(0,Math.floor((f.first_index||0)/50)*50);
  setTab("activity");
  return loadActivity().then(()=>selectCall(seq));
}
$("behaviour-nav").onclick=()=>showBehaviour();
$("behaviour-filters").onsubmit=e=> {
  e.preventDefault();
  loadBehaviour();
};
$("behaviour-search").oninput=debounce(()=>loadBehaviour());
$("behaviour-server").onchange=()=>loadBehaviour();
$("behaviour-window").onchange=()=>loadBehaviour();
$("behaviour-from").onchange=()=> {
  $("behaviour-window").value="";
  loadBehaviour();
};
$("behaviour-to").onchange=()=> {
  $("behaviour-window").value="";
  loadBehaviour();
};
$("run-delta").onclick=runDelta;
$("lab-save").onclick=saveLabCaseToLibrary;
$("lab-nav").onclick=()=>showLab();
$("lab-search").oninput=debounce(()=>{
  if(labIndex)renderLab(labIndex);
});
$("lab-reload").onclick=()=>loadLab();
for(const tab of document.querySelectorAll("[data-labtab]")) {
  tab.onclick=()=>setLabTab(tab.dataset.labtab);
  tab.onkeydown=e=>{
    if(e.key!=="ArrowRight"&&e.key!=="ArrowLeft")return;
    const order=["cases","suites","help"];
    const i=order.indexOf(tab.dataset.labtab);
    const next=order[(i+(e.key==="ArrowRight"?1:order.length-1))%order.length];
    setLabTab(next);
    $(`lab-tab-${next}`).focus();
  };
}

/* ------------------------------------------------------------------
   Regression lab (M8.1).

   The browser never decides an outcome. Every PASS, FAIL and INCONCLUSIVE
   below comes from /api/lab*, which runs internal/lab — the same code the
   `tapelog lab` CLI uses. The interface exists to make cases findable, runnable
   and explainable.
   ------------------------------------------------------------------ */
let labIndex=null,labRequest=0,labRunRequest=0,labCaseFile="",labSuiteFile="",labResults={},labTab="cases",pendingLab=null;
const LAB_STATUS_HINT= {
  PASS:"The expectation was evaluated and held.",
  FAIL:"Eligible evidence contradicts the expectation.",
  INCONCLUSIVE:"The evidence could not decide it. This is not a pass."
};
const LAB_CODE_LABELS= {
  no_calls:"No matching call reached the boundary",
  no_results:"No recorded result to judge",
  redacted_content:"Redacted content hides the answer",
  missing_decision:"A call has no policy decision",
  no_denials:"No deny decision to test the invariant",
  short_sequence:"Not enough recorded results for the sequence",
  incomplete_recording:"The recording has no complete start and end",
  damaged_recording:"The recording is damaged, empty, or above the lab's event limit",
  revision_changed:"The recording changed since the case was saved",
  evidence_unreadable:"The recording could not be read",
  invalid_case:"The case definition could not be used"
};
function labStatusClass(status) {
  return status==="PASS"?"allow":status==="FAIL"?"failed":status==="INCONCLUSIVE"?"confirmation":"";
}
function labStatusPill(status) {
  const n=el("span","badge "+labStatusClass(status),status);
  n.title=LAB_STATUS_HINT[status]||"";
  return n;
}
function labCodeLabel(code) {
  return LAB_CODE_LABELS[code]||code;
}
function showLab(updateHash=true) {
  ++labRequest;
  showView("lab");
  $("breadcrumb").textContent="Workspace / Regression lab";
  clearError();
  if(updateHash)setWorkspaceRoute("#lab"+(labTab==="suites"?"&tab=suites":""));
  pendingLab=labLink();
  loadLab();
}
// labLink reads #lab&case=<file> or #lab&suite=<file>, so a specific run can be
// linked to from a review, a ticket or a CI log.
function labLink() {
  if(!location.hash.startsWith("#lab"))return null;
  const cut=location.hash.indexOf("&");
  if(cut<0)return null;
  const query=new URLSearchParams(location.hash.slice(cut+1));
  const c=query.get("case"),s=query.get("suite");
  if(!c&&!s)return null;
  return {
    caseFile:c||"",suiteFile:s||""
  };
}
function setLabTab(name,updateHash=true) {
  if(!["cases","suites","help"].includes(name))name="cases";
  labTab=name;
  $("lab-nav").classList.toggle("active",activeView==="lab"&&name!=="suites");
  $("suites-nav").classList.toggle("active",activeView==="lab"&&name==="suites");
  $("lab-page-title").textContent=name==="suites"?"Test suites.":"Regression cases.";
  if(activeView==="lab"&&updateHash)setWorkspaceRoute("#lab&tab="+name);
  for(const tab of document.querySelectorAll("[data-labtab]")) {
    const on=tab.dataset.labtab===name;
    tab.classList.toggle("selected",on);
    tab.setAttribute("aria-selected",String(on));
    tab.tabIndex=on?0:-1;
    $(`lab-${tab.dataset.labtab}-panel`).hidden=!on;
  }
  if(name==="cases"&&labCaseFile)restoreLabRun("case",labCaseFile);
  else if(name==="suites"&&labSuiteFile)restoreLabRun("suite",labSuiteFile);
}
async function loadLab() {
  const request=++labRequest;
  try {
    const data=await api("/api/lab");
    if(request!==labRequest)return;
    labIndex=data;
    renderLab(data);
  } catch(e) {
    if(request!==labRequest)return;
    const notes=$("lab-notes");
    notes.replaceChildren();
    notice(notes,e.message,"warning");
    $("lab-case-list").replaceChildren();
    $("lab-suite-list").replaceChildren();
  }
}
function renderLab(index) {
  const notes=$("lab-notes");
  notes.replaceChildren();
  for(const note of index.notes||[])notes.append(el("p","behaviour-note",note));
  const invalid=(index.cases||[]).filter(c=>c.invalid).length;
  const missing=(index.suites||[]).reduce((n,s)=>n+(s.missing||[]).length,0);
  const savedCount=(index.cases||[]).length, suiteTotal=(index.suites||[]).length;
  stats($("lab-stats"),[[savedCount,savedCount===1?"Saved case":"Saved cases"],[suiteTotal,suiteTotal===1?"Suite":"Suites"],[invalid,"Cases needing a fix"],[missing,"Suite entries missing"]]);
  renderLabCases(index);
  renderLabSuites(index);
  renderLabHelp(index);
  const caseCount=(index.cases||[]).length, suiteCount=(index.suites||[]).length;
  $("lab-case-count").textContent=caseCount+(caseCount===1?" case":" cases");
  $("lab-suite-count").textContent=suiteCount+(suiteCount===1?" suite":" suites");
  if(!labCaseFile)empty($("lab-case-detail"),"Open a case","Select a saved case to read what it asserts, run it against its evidence, and jump to the recorded calls behind it.");
  if(!labSuiteFile&&!suiteDraft)empty($("lab-suite-detail"),"Open a suite","A suite runs several cases in order and reports one overall result.");
  if(pendingLab) {
    const wanted=pendingLab;
    pendingLab=null;
    const has=(list,file)=>list.some(x=>x.file===file);
    if(wanted.suiteFile&&has(index.suites||[],wanted.suiteFile)) {
      setLabTab("suites",false);
      openLabSuite(wanted.suiteFile);
      restoreLabRun("suite",wanted.suiteFile);
    } else if(wanted.caseFile&&has(index.cases||[],wanted.caseFile)) {
      setLabTab("cases",false);
      openLabCase(wanted.caseFile);
      restoreLabRun("case",wanted.caseFile);
    } else {
      notice(notes,"That link names a case or suite that is not in this directory any more.","warning");
    }
  }
}
function labMatches(text) {
  const q=$("lab-search").value.trim().toLowerCase();
  if(!q)return true;
  return text.toLowerCase().includes(q);
}
// labExpectationText restates one expectation in words, so a reader never has to
// interpret the JSON to know what a case asserts.
function labExpectationText(x) {
  const tool=x.tool?"“"+x.tool+"”":"this boundary";
  switch(x.kind) {
    case "answered":return tool+" was answered with a recorded result.";
    case "denied":return tool+" has a call whose final recorded policy decision is deny.";
    case "attempted":return tool+" reached the boundary at least once.";
    case "no_attempts":return tool+" never reached the boundary.";
    case "attempt_ceiling":return tool+" reached the boundary at most "+(x.maximum??0)+" time(s).";
    case "result_contains":return "A recorded result of "+tool+" contains “"+x.text+"”.";
    case "result_never_contains":return "No recorded result of "+tool+" contains “"+x.text+"”.";
    case "sequence":return "Recorded results include the sequence "+(x.sequence||[]).map(t=>"“"+t+"”").join(" → ")+".";
    case "no_deny_bypassed":return "No denied call produced a recorded result.";
    default:return x.kind;
  }
}
async function openLabCase(file) {
  ++labRunRequest;
  labCaseFile=file;
  setLabTab("cases",false);
  const target="#lab&case="+encodeURIComponent(file);
  if(!location.hash.startsWith(target+"&outcome="))setWorkspaceRoute(target);
  renderLabCases(labIndex||{cases:[]});
  const detail=$("lab-case-detail");
  detail.replaceChildren(el("p","muted","Opening…"));
  try {
    const data=await api("/api/lab/case?"+params( {file}));
    if(labCaseFile!==file)return;
    if(!workspaceRecordings.length) {
      const recordings=await api("/api/sessions?limit=100");
      if(recordings.total>100) {
        const older=await api("/api/sessions?offset=100&limit=100");
        recordings.sessions.push(...older.sessions);
      }
      workspaceRecordings=recordings.sessions||[];
    }
    if(labCaseFile!==file)return;
    renderLabCaseDetail(data);
    restoreLabRun("case",file);
  } catch(e) {
    if(labCaseFile!==file)return;
    detail.replaceChildren();
    notice(detail,e.message,"warning");
  }
}
function renderLabCaseDetail(data) {
  const detail=$("lab-case-detail");
  const c=data.case;
  detail.replaceChildren();
  detail.append(el("div","eyebrow","SAVED CASE"),el("h2","",c.title||c.file),el("p","mono muted",c.file));
  if(c.description)detail.append(el("p","",c.description));
  const evidence=el("p","");
  evidence.append(el("span","muted","Evidence: "),el("code","",c.evidence||"none named"));
  if(!data.evidence.present)evidence.append(el("span","badge failed","missing"));
  else if(data.evidence.matches_saved===false)evidence.append(el("span","badge confirmation","changed since saved"));
  detail.append(evidence);
  if(c.reference)detail.append(el("p","muted","Reference recording: "+c.reference));
  detail.append(el("p","selection-note",c.eligibility==="complete-boundary"?"Complete-boundary eligibility: absent calls can decide existence and count checks. Missing results cannot prove text absence.":"M8.1 eligibility: preserved for this existing case. No matching calls produce INCONCLUSIVE."));
  const list=el("ul","lab-expectations");
  for(const x of c.expectations||[])list.append(el("li","",labExpectationText(x)));
  detail.append(el("h3","","What this case asserts"),list);
  if(data.invalid)notice(detail,data.invalid,"warning");
  const actions=el("div","actions");
  const candidate=el("select","lab-candidate-choice");
  candidate.setAttribute("aria-label","Evidence to run this case against");
  candidate.add(new Option("Original evidence · "+c.evidence,""));
  // A directory can hold recordings that share a name (one harness, many runs),
  // so the file is always shown beside it: two identical options are unusable.
  for(const r of workspaceRecordings)if(r.file!==c.evidence)candidate.add(new Option("Candidate · "+(r.name||r.file)+" · "+r.file,r.file));
  detail.append(el("label","","Run against"),candidate);
  actions.append(button("Run this case →",()=>runLabCase(c.file,candidate.value),"primary"));
  if(data.evidence.present)actions.append(button("Open saved evidence →",()=>openBehaviourCall(c.evidence,c.evidence_seqs?.[0]||0)));
  if(c.reference)actions.append(button("Compare with reference",async()=>{await showComparisons(c.reference,c.evidence);await runWorkspaceComparison();}));
  actions.append(button("Copy CLI command",()=>labCopy("tapelog lab run --dir . --case "+c.file)));
  detail.append(actions);
  const suiteChoice=el("select");
  suiteChoice.setAttribute("aria-label","Suite for this case");
  suiteChoice.add(new Option("Create a new suite",""));
  for(const s of labIndex?.suites||[])if(!s.invalid)suiteChoice.add(new Option(s.title||s.file,s.file));
  const grouping=el("div","actions");
  grouping.append(suiteChoice,button("Add to suite…",()=> {
    setLabTab("suites");
    if(suiteChoice.value)openLabSuite(suiteChoice.value);else newSuiteDraft();
    if(suiteDraft&&!suiteDraft.cases.includes(c.file)) { suiteDraft.cases.push(c.file);suiteDraft.dirty=true; }
    renderSuiteEditor();
    $("lab-suite-detail").scrollIntoView({block:"start"});
  }));
  detail.append(grouping);
  const raw=el("details");
  raw.append(el("summary","","Case definition (JSON)"),el("pre","lab-json",data.json));
  detail.append(raw);
}
async function runLabCase(file,candidate) {
  const request=++labRunRequest;
  $("lab-run-target").textContent="Running "+file+"…";
  const out=$("lab-run");
  out.replaceChildren(el("p","muted","Running the offline runner. No tool is executed."));
  try {
    const data=await api("/api/lab/run", {
      method:"POST",headers: {
        "Content-Type":"application/json"
      }
      ,body:JSON.stringify( {
        file,candidate:candidate||""
      }
      )
    }
    );
    if(request!==labRunRequest)return;
    labResults["case:"+file]=data.result;
    labRunCache["case:"+file]=data;
    setWorkspaceRoute("#lab&case="+encodeURIComponent(file)+"&outcome=latest");
    renderCaseResult(data);
    $("lab-run-title").scrollIntoView({block:"start"});
    renderLabCases(labIndex||{cases:[]});
  } catch(e) {
    if(request!==labRunRequest)return;
    $("lab-run-target").textContent="The run did not start.";
    out.replaceChildren();
    notice(out,e.message,"warning");
  }
}
function renderCaseResult(data) {
  const out=$("lab-run"),r=data.result;
  out.replaceChildren();
  $("lab-run-target").textContent=r.title+" · "+r.evidence;
  const head=el("div","lab-result-head");
  head.append(labStatusPill(r.status),el("strong",""," "+(LAB_STATUS_HINT[r.status]||"")));
  out.append(head);
  const table=el("table","data-table");
  const thead=el("thead"),tbody=el("tbody"),hr=el("tr");
  for(const h of ["Expectation","Outcome","Why"])hr.append(el("th","",h));
  thead.append(hr);table.append(thead);
  for(const c of r.checks||[]) {
    const tr=el("tr");
    const why=el("td");
    why.append(el("span","",c.detail));
    if(c.code)why.append(el("span","muted"," ("+labCodeLabel(c.code)+")"));
    tr.append(el("td","",data.case?.expectations?.[tbody.children.length]?labExpectationText(data.case.expectations[tbody.children.length]):c.kind),el("td","lab-outcome",c.status),why);
    tbody.append(tr);
  }
  table.append(tbody);
  out.append(table);
  if(r.status==="INCONCLUSIVE")notice(out,"This is not a pass. The recorded evidence could not decide the expectation, so the case proves nothing yet.","warning");
  if(r.status==="FAIL")notice(out,"The recorded evidence contradicts the expectation. Open the evidence to see the call that did it.","error");
  const actions=el("div","actions");
  if(r.evidence)actions.append(button("Open the evidence →",()=>openBehaviourCall(r.evidence,r.evidence===data.case?.evidence?data.case.evidence_seqs?.[0]||0:0)));
  if(data.case?.reference)actions.append(button("Compare with reference",async()=>{await showComparisons(data.case.reference,r.evidence);await runWorkspaceComparison();}));
  actions.append(button("Copy CLI command",()=>labCopy(data.command||"")));
  out.append(actions);
  if(data.comparison_status&&data.comparison_status!=="not_requested")renderComparison(out,data.comparison,data.comparison_status);
  out.append(el("p","muted",r.note));
  if(data.command)out.append(el("p","muted mono","Reproduce it: "+data.command));
}

function renderLabCases(index) {
  const list=$("lab-case-list");
  list.replaceChildren();
  const cases=(index.cases||[]).filter(c=>labMatches([c.file,c.title,c.description,c.evidence,(c.expectations||[]).map(e=>e.kind+" "+(e.tool||"")).join(" ")].join(" ")));
  if(!cases.length) {
    empty(list,(index.cases||[]).length?"No case matches that search":"No saved cases yet","Open a recording, select the call you care about, and save a case from the session's Create case tab. Cases live beside your recordings as plain JSON you can review and commit.");
    list.append(button("Browse sessions →",showLibrary,"primary"));
    return;
  }
  for(const c of cases) {
    const row=button("",()=>openLabCase(c.file),"lab-row"+(labCaseFile===c.file?" selected":""));
    const head=el("div","lab-row-head");
    head.append(el("strong","",c.title||c.file));
    const last=labResults["case:"+c.file];
    if(last)head.append(labStatusPill(last.status));
    row.append(head);
    if(c.description)row.append(el("p","muted",c.description));
    const meta=el("div","lab-row-meta");
    meta.append(el("span","mono",c.evidence||"no evidence named"));
    if(!c.evidence_present)meta.append(el("span","badge failed","evidence missing"));
    if(c.invalid)meta.append(el("span","badge failed","needs a fix"));
    const kinds=(c.expectations||[]).map(labExpectationText).join(" · ");
    if(kinds)meta.append(el("span","muted",kinds));
    row.append(meta);
    if(c.invalid)row.append(el("p","text-warn",c.invalid));
    list.append(row);
  }
}
// --- comparison --------------------------------------------------------
// Differences are shown, never scored: a new failure group in the candidate is a
// prompt to look, not a verdict on the case.
function renderComparison(parent,comparison,status) {
  const box=el("div","lab-comparison");
  box.append(el("h4","","Reference comparison · "+status));
  if(!comparison) {
    box.append(el("p","muted","The comparison could not be produced for these recordings."));
    parent.append(box);
    return;
  }
  const rows=[
    ["New failure groups",(comparison.new_failures||[]).length],
    ["Failure groups no longer present",(comparison.resolved_failures||[]).length],
    ["New denial rules",(comparison.new_denials||[]).length],
    ["Denial rules no longer present",(comparison.resolved_denials||[]).length],
    ["New repeat patterns",(comparison.new_repeats||[]).length],
    ["Repeat patterns no longer present",(comparison.resolved_repeats||[]).length]
  ];
  const table=el("table","data-table"),tbody=el("tbody");
  for(const [label,count] of rows) {
    const tr=el("tr");
    tr.append(el("td","",label),el("td","num",count));
    tbody.append(tr);
  }
  table.append(tbody);
  box.append(table);
  for(const caveat of comparison.caveats||[])box.append(el("p","muted",caveat));
  box.append(el("p","muted","A difference is an observation. It never changes the case outcome."));
  parent.append(box);
}

// --- suites ------------------------------------------------------------
let suiteDraft=null;
function renderLabSuites(index) {
  const list=$("lab-suite-list");
  list.replaceChildren();
  const actions=el("div","actions");
  actions.append(button("New suite +",()=>newSuiteDraft(),"secondary"));
  list.append(actions);
  const suites=(index.suites||[]).filter(s=>labMatches([s.file,s.title,(s.cases||[]).join(" ")].join(" ")));
  if(!suites.length) {
    empty(list,(index.suites||[]).length?"No suite matches that search":"No suites yet","A suite runs several cases in order and reports one overall result. Create one, add the cases you trust, and run it after a change.");
    list.append(actions);
    return;
  }
  for(const s of suites) {
    const row=button("",()=>openLabSuite(s.file),"lab-row"+(labSuiteFile===s.file?" selected":""));
    const head=el("div","lab-row-head");
    head.append(el("strong","",s.title||s.file));
    const last=labResults["suite:"+s.file];
    if(last)head.append(labStatusPill(last.status));
    row.append(head);
    const meta=el("div","lab-row-meta");
    meta.append(el("span","muted",(s.cases||[]).length+" case(s)"),el("span","mono",s.file));
    if((s.missing||[]).length)meta.append(el("span","badge failed",(s.missing||[]).length+" missing"));
    if(s.invalid)meta.append(el("span","badge failed","needs a fix"));
    row.append(meta);
    list.append(row);
  }
}
function newSuiteDraft() {
  if(!confirmDiscardSuiteEdits())return;
  ++labRunRequest;
  suiteDraft= {file:"",title:"",cases:[],revision:"",isNew:true,missing:[]};
  labSuiteFile="";
  setLabTab("suites");
  if(labIndex)renderLabSuites(labIndex);
  renderSuiteEditor();
}
function confirmDiscardSuiteEdits() {
  if(!(suiteDraft&&suiteDraft.dirty))return true;
  if(typeof confirm!=="function")return true;
  return confirm("Discard unsaved changes to \u201c"+(suiteDraft.title||"this suite")+"\u201d?");
}
function openLabSuite(file) {
  // Returning to the same suite keeps the draft: navigating away must not
  // silently throw the owner's edits away.
  if(suiteDraft&&suiteDraft.file===file&&suiteDraft.dirty) {
    renderSuiteEditor();
    return;
  }
  if(!confirmDiscardSuiteEdits()) {
    // The hash already moved to the declined suite; put it back so the address
    // bar, the list highlight and the editor all describe the same suite.
    if(labSuiteFile&&location.hash!=="#lab&suite="+encodeURIComponent(labSuiteFile)) {
      history.replaceState(null,"","#lab&suite="+encodeURIComponent(labSuiteFile));
    }
    return;
  }
  ++labRunRequest;
  labSuiteFile=file;
  setLabTab("suites",false);
  const target="#lab&suite="+encodeURIComponent(file);
  if(!location.hash.startsWith(target+"&outcome="))setWorkspaceRoute(target);
  if(labIndex)renderLabSuites(labIndex);
  const entry=((labIndex&&labIndex.suites)||[]).find(s=>s.file===file);
  if(!entry) {
    const detail=$("lab-suite-detail");
    detail.replaceChildren();
    notice(detail,"This suite is no longer in the library. Reload it.","warning");
    return;
  }
  suiteDraft= {file:entry.file,title:entry.title,cases:(entry.cases||[]).slice(),revision:entry.revision,isNew:false,missing:entry.missing||[],invalid:entry.invalid||""};
  renderSuiteEditor();
  restoreLabRun("suite",file);
}
function suiteCommand(file) {
  return "tapelog lab suite --dir . --suite "+file;
}
function refreshSuiteState() {
  const draft=suiteDraft;
  if(!draft)return;
  const badge=$("suite-save-state");
  if(badge) {
    // A new suite has no file yet: "Saved" would be a lie until the first save.
    badge.className="badge suite-save-state "+(draft.isNew?"":draft.dirty?"denied":"success");
    badge.textContent=draft.isNew?"Not saved yet":draft.dirty?"Unsaved changes":"Saved";
  }
  const count=$("suite-case-count");
  if(count)count.textContent=draft.cases.length+(draft.cases.length===1?" case":" cases");
  const run=$("suite-run-saved");
  if(run) {
    run.disabled=!!draft.dirty;
    run.title=draft.dirty?"Save your changes before running the saved file.":"";
  }
  const note=$("suite-run-note");
  if(note)note.textContent=draft.dirty?"Save your changes before running. Run executes the saved file, not this draft.":"Run executes the saved file named by the CLI command below.";
}
function renderSuiteEditor() {
  const detail=$("lab-suite-detail");
  const draft=suiteDraft;
  const grid=detail.closest?detail.closest(".lab-grid"):null;
  detail.replaceChildren();
  if(grid)grid.classList.toggle("suite-editing",!!draft);
  if(!draft) {
    empty(detail,"Choose a suite","Open a suite to see its cases, or create a new one.");
    return;
  }
  detail.append(el("div","eyebrow",draft.isNew?"NEW SUITE":"SAVED SUITE"));
  const head=el("div","suite-head");
  const name=el("h2","suite-name",draft.title||"Untitled suite");
  name.id="suite-name";
  head.append(name);
  const status=el("div","suite-status");
  status.id="suite-status";status.setAttribute("role","status");status.setAttribute("aria-live","polite");
  const countBadge=el("span","badge suite-case-count",draft.cases.length+(draft.cases.length===1?" case":" cases"));
  countBadge.id="suite-case-count";
  status.append(countBadge);
  const saveBadge=el("span","badge suite-save-state "+(draft.isNew?"":draft.dirty?"denied":"success"),draft.isNew?"Not saved yet":draft.dirty?"Unsaved changes":"Saved");
  saveBadge.id="suite-save-state";
  status.append(saveBadge);
  head.append(status);
  detail.append(head);
  const field=el("label","suite-title-field");
  field.append(el("span","suite-field-label","Suite title"));
  const title=el("input","suite-title-input");
  title.type="text";
  title.value=draft.title;
  title.maxLength=200;
  title.placeholder="e.g. Nightly boundary checks";
  title.setAttribute("aria-describedby","suite-status");
  title.oninput=()=> {
    draft.title=title.value;
    draft.dirty=true;
    const heading=$("suite-name");
    if(heading)heading.textContent=draft.title||"Untitled suite";
    refreshSuiteState();
  };
  field.append(title);
  detail.append(field);
  detail.append(el("p","suite-lede","Each case states a claim about recorded evidence; the verdict comes from running it. Cases run in this order."));
  const list=el("ul","suite-case-list");
  if(!draft.cases.length)list.append(el("li","suite-case-empty","No cases yet. Add at least one before saving."));
  draft.cases.forEach((name,index)=> {
    const entry=((labIndex&&labIndex.cases)||[]).find(c=>c.file===name);
    const li=el("li","suite-case");
    li.append(el("span","suite-case-index",String(index+1).padStart(2,"0")));
    const text=el("div","suite-case-text");
    text.append(el("span","suite-case-title",entry?(entry.title||entry.file):name));
    if(entry&&entry.description)text.append(el("span","suite-case-desc",entry.description));
    const meta=el("span","suite-case-file");
    meta.textContent=name+(entry&&entry.evidence?" · "+entry.evidence:"")+(entry?"":" · not in this directory");
    text.append(meta);
    li.append(text);
    const remove=button("Remove",()=> {
      draft.cases=draft.cases.filter(x=>x!==name);
      draft.dirty=true;
      renderSuiteEditor();
    }
    ,"quiet suite-case-remove");
    remove.setAttribute("aria-label","Remove case "+(entry&&entry.title?entry.title:name)+" from this suite");
    li.append(remove);
    list.append(li);
  }
  );
  detail.append(list);
  const available=((labIndex&&labIndex.cases)||[]).filter(c=>!draft.cases.includes(c.file)&&!c.invalid);
  if(available.length) {
    const add=el("div","suite-add");
    const label=el("label","suite-field-label","Add a case");
    label.setAttribute("for","lab-add-case");
    add.append(label);
    const row=el("div","suite-add-row");
    const select=el("select","");
    select.id="lab-add-case";
    for(const c of available)select.add(new Option(c.title||c.file,c.file));
    row.append(select);
    row.append(button("Add to suite",()=> {
      const chosen=select.value;
      if(chosen&&!draft.cases.includes(chosen))draft.cases.push(chosen);
      else {
        notice(detail,"That case is already in this suite.","warning");
        return;
      }
      draft.dirty=true;
      renderSuiteEditor();
    }
    ,"secondary"));
    add.append(row);
    detail.append(add);
  } else {
    detail.append(el("p","suite-add-empty","Every saved case is already in this suite."));
  }
  if(draft.invalid)notice(detail,draft.invalid,"warning");
  if((draft.missing||[]).length)notice(detail,"Referenced but not present: "+draft.missing.join(", ")+". The suite still runs, and each missing case is reported as inconclusive.","warning");
  const actions=el("div","suite-actions");
  actions.append(button(draft.isNew?"Save suite":"Save changes",()=>saveSuiteDraft(),"primary"));
  if(!draft.isNew) {
    const run=button("Run saved suite →",()=>runLabSuite(draft.file),"secondary");
    run.id="suite-run-saved";
    run.disabled=!!draft.dirty;
    actions.append(run);
  }
  detail.append(actions);
  const note=el("p","suite-run-note");
  note.id="suite-run-note";
  detail.append(note);
  if(!draft.isNew) {
    const cli=el("div","suite-cli");
    const cliHead=el("div","suite-cli-head");
    cliHead.append(el("span","suite-cli-label","Reproduce from a terminal"));
    const command=suiteCommand(draft.file);
    const copy=button("Copy",async()=> {
      try {
        await navigator.clipboard.writeText(command);
        // A previous failed attempt may have left a "copy it yourself" banner.
        for(const child of [...detail.children]) {
          if(child.className==="banner warning"&&/^Copy this command yourself/.test(child.textContent))child.remove();
        }
        copy.textContent="Copied";
        setTimeout(()=> {
          copy.textContent="Copy";
        }
        ,1500);
      } catch(e) {
        notice(detail,"Copy this command yourself: "+command,"warning");
      }
    }
    ,"quiet suite-cli-copy");
    copy.setAttribute("aria-label","Copy the CLI command for this suite");
    cliHead.append(copy);
    cli.append(cliHead,el("code","suite-cli-command",command));
    detail.append(cli);
  }
  refreshSuiteState();
}
function clearSuiteBanners(detail) {
  for(const child of [...detail.children])if(String(child.className||"").startsWith("banner"))child.remove();
}
async function saveSuiteDraft() {
  const detail=$("lab-suite-detail");
  const draft=suiteDraft;
  if(!draft)return;
  const fail=text=> {
    // One failure at a time: a stale "Saved as …" above an error reads as success.
    clearSuiteBanners(detail);
    const banner=el("div","banner warning",text);
    banner.setAttribute("role","alert");
    detail.prepend(banner);
    if(banner.scrollIntoView)banner.scrollIntoView({block:"nearest"});
  };
  if(!draft.title.trim()) {
    fail("Give the suite a title before saving.");
    return;
  }
  if(!draft.cases.length) {
    fail("Add at least one case before saving.");
    return;
  }
  try {
    const data=await api("/api/lab/suite", {
      method:"POST",headers: {
        "Content-Type":"application/json"
      }
      ,body:JSON.stringify( {
        file:draft.file,replaces:draft.revision,suite: {
          version:1,title:draft.title.trim(),cases:draft.cases
        }
      }
      )
    }
    );
    // The server accepted the suite. Take the new state from its response so a
    // failed index reload cannot reopen the pre-save revision as "Saved".
    clearSuiteBanners(detail);
    draft.file=data.file;
    draft.title=draft.title.trim();
    draft.dirty=false;
    draft.isNew=false;
    if(data.revision)draft.revision=data.revision;
    if(Array.isArray(data.missing))draft.missing=data.missing;
    if(labIndex) {
      const suites=labIndex.suites||(labIndex.suites=[]);
      const entry=suites.find(s=>s.file===data.file);
      const saved= {file:data.file,title:draft.title,cases:draft.cases.slice(),revision:draft.revision,missing:draft.missing||[]};
      if(entry)Object.assign(entry,saved);
      else suites.push(saved);
      renderLabSuites(labIndex);
    }
    if(labSuiteFile!==data.file) {
      labSuiteFile=data.file;
      setWorkspaceRoute("#lab&suite="+encodeURIComponent(data.file));
    }
    renderSuiteEditor();
    notice(detail,"Saved as "+data.file+".","");
    loadLab().catch(()=> {});
  } catch(e) {
    fail(e.message);
  }
}
async function runLabSuite(file) {
  const request=++labRunRequest;
  $("lab-run-target").textContent="Running suite "+file+"…";
  $("lab-run").replaceChildren(el("p","muted","Running every case in order. No tool is executed."));
  try {
    const data=await api("/api/lab/suite/run", {
      method:"POST",headers: {
        "Content-Type":"application/json"
      }
      ,body:JSON.stringify( {
        file
      }
      )
    }
    );
    if(request!==labRunRequest)return;
    labResults["suite:"+file]=data.suite;
    labRunCache["suite:"+file]=data;
    setWorkspaceRoute("#lab&suite="+encodeURIComponent(file)+"&outcome=latest");
    renderSuiteResult(data);
    $("lab-run-title").scrollIntoView({block:"start"});
    if(labIndex)renderLabSuites(labIndex);
  } catch(e) {
    if(request!==labRunRequest)return;
    $("lab-run-target").textContent="The suite did not run.";
    $("lab-run").replaceChildren();
    notice($("lab-run"),e.message,"warning");
  }
}
function renderSuiteResult(data) {
  const out=$("lab-run"),s=data.suite;
  out.replaceChildren();
  $("lab-run-target").textContent=s.title+" · "+data.file;
  const head=el("div","lab-result-head");
  head.append(labStatusPill(s.status),el("strong",""," "+(LAB_STATUS_HINT[s.status]||"")));
  out.append(head);
  const counts=el("div","stat-strip");
  stats(counts,[[s.pass,"Passed"],[s.fail,"Failed"],[s.inconclusive,"Inconclusive"],[s.invalid||0,"Unusable case definitions"]]);
  out.append(counts);
  const table=el("table","data-table"),thead=el("thead"),tbody=el("tbody");
  const hr=el("tr");
  for(const h of ["Case","Outcome","Evidence","Why"])hr.append(el("th","",h));
  thead.append(hr);table.append(thead);
  for(const [i,c] of (s.cases||[]).entries()) {
    const tr=el("tr");
    const caseFile=data.case_files?.[i];
    const entry=(labIndex?.cases||[]).find(x=>x.file===caseFile);
    const evidence=el("td");
    if(c.evidence)evidence.append(button(c.evidence,()=>openBehaviourCall(c.evidence,c.evidence===entry?.evidence?entry.evidence_seqs?.[0]||0:0),"link-button"));
    else evidence.append(el("span","muted","none"));
    const why=el("td");
    for(const ch of c.checks||[])why.append(el("div","",ch.detail+(ch.code?" ("+labCodeLabel(ch.code)+")":"")));
    const title=el("td");
    if(caseFile)title.append(button(c.title||caseFile,()=>openLabCase(caseFile),"link-button"));
    else title.append(el("span","",c.title||""));
    tr.append(title,el("td","lab-outcome",c.status),evidence,why);
    tbody.append(tr);
  }
  table.append(tbody);
  out.append(table);
  if(s.status==="INCONCLUSIVE")notice(out,"At least one case could not be decided by its evidence. An inconclusive suite is not a passing suite.","warning");
  if(s.status==="FAIL")notice(out,"At least one case contradicts its expectation.","error");
  for(const comparison of data.comparisons||[]) {
    const box=el("div","lab-comparison");
    box.append(el("h4","",comparison.title+" · "+comparison.reference+" → "+comparison.candidate));
    renderComparison(box,comparison.comparison,comparison.status);
    out.append(box);
  }
  if(data.comparisons_truncated)out.append(el("p","muted","Only the first "+20+" comparisons are shown."));
  if(data.command)out.append(el("p","muted mono","Reproduce it: "+data.command));
}
function renderLabHelp() {
  const box=$("lab-help");
  box.replaceChildren();
  const rows=[
    ["A case","One recording plus the expectations you want to keep. It names the recording instead of copying it, so no recorded argument or result is duplicated into the case file."],
    ["A suite","Several cases run in order with one overall result. The suite reports the worst outcome it saw: FAIL beats INCONCLUSIVE, which beats PASS."],
    ["PASS","The expectation was evaluated against eligible evidence and held."],
    ["FAIL","Eligible recorded evidence contradicts the expectation. This does not by itself prove a change from a previous run."],
    ["INCONCLUSIVE","The evidence could not decide it: no matching call, no recorded result, redacted content, a damaged or incomplete recording, or an unusable case definition. It is never a pass."],
    ["Why so strict","A boundary that answered one call while refusing three others still answered a call. Tapelog distinguishes a claim that needs one witness from a claim that needs every result."],
    ["What is never done","The runner does not execute tools, contact servers or re-run historical calls. It reads what the recorder wrote."]
  ];
  for(const [term,meaning] of rows) {
    const row=el("div","definition-row");
    row.append(el("dt","",term),el("dd","",meaning));
    box.append(row);
  }
}
// labCopy uses the clipboard when the browser allows it, and shows the text when
// it does not, rather than failing silently.
async function labCopy(text) {
  if(!text)return;
  try {
    await navigator.clipboard.writeText(text);
    notice($("lab-run"),"Copied: "+text,"");
  } catch(e) {
    notice($("lab-run"),"Copy this command yourself: "+text,"warning");
  }
}

/* M9 workspace: route/context orchestration only. Evidence, policy and test
   decisions continue to come from the existing server engines. */
let activeView="library",restoringRoute=false,restoringHash="",routeRequest=0,returnRoute="";
let workspaceRecordings=[],pendingComparison=null,comparisonEpoch=0;
const labRunCache=Object.create(null);

function validEventSeq(value) {
  const n=Number(value);
  return Number.isSafeInteger(n)&&n>0?n:0;
}
function sessionHash(file,tab="activity",seq=0) {
  const q=new URLSearchParams({session:file,tab});
  if(validEventSeq(seq))q.set("seq",String(seq));
  return "#"+q;
}
function setWorkspaceRoute(hash,replace=false) {
  if(restoringRoute||location.hash===hash)return;
  history[replace?"replaceState":"pushState"](null,"",location.pathname+location.search+hash);
}
function workspaceRouteLabel(hash) {
  if(hash.startsWith("#lab"))return hash.includes("suite=")?"suite outcome":"regression case";
  if(hash.startsWith("#compare"))return "comparison";
  if(hash.startsWith("#behaviour"))return "behaviour analysis";
  if(hash.startsWith("#studio"))return "policy draft";
  if(hash.startsWith("#investigations"))return "saved investigations";
  return "sessions";
}
async function restoreWorkspaceRoute() {
  const hash=location.hash;
  if(hash==="#main")return;
  if(restoringRoute&&restoringHash===hash)return;
  const request=++routeRequest;
  restoringRoute=true;
  restoringHash=hash;
  try {
    if(hash.startsWith("#session=")) {
      const target=hashTarget();
      await openSession(target.file,false);
    } else if(hash.startsWith("#behaviour&")&&new URLSearchParams(hash.slice(hash.indexOf("&")+1)).get("conversation")) {
      // A conversation deep link: the Behaviour view opens the conversation.
      showBehaviour(false);
    } else if(hash.startsWith("#compare")||hash.startsWith("#behaviour&")) {
      const q=new URLSearchParams(hash.slice(hash.indexOf("&")+1));
      await showComparisons(q.get("a")||"",q.get("b")||"",false);
      if(q.get("a")&&q.get("b"))await runWorkspaceComparison();
    } else if(hash.startsWith("#behaviour"))showBehaviour(false);
    else if(hash.startsWith("#lab")) {
      const q=new URLSearchParams(hash.slice(hash.indexOf("&")+1));
      setLabTab(q.get("suite")?"suites":q.get("tab")||"cases");
      showLab(false);
    } else if(hash.startsWith("#studio"))await openStudio(decodeURIComponent(hash.split("=").slice(1).join("=")));
    else if(hash==="#investigations")showInvestigations(false);
    else showLibrary();
  } catch(e) { error(e); }
  finally { if(request===routeRequest)restoringRoute=false; }
}
function updateWorkspaceContext() {
  const out=$("workspace-context");
  out.replaceChildren();
  out.hidden=!currentFile;
  if(!currentFile)return;
  const label=el("div","context-recording");
  label.append(el("span","eyebrow","CURRENT INVESTIGATION"),el("strong","",currentInfo?.name||currentFile));
  label.append(el("span","muted",inspectedEvent&&inspectedEvent!==selectedCall?.seq?"Event #"+inspectedEvent+" · all recorded events":selectedCall?selectedCall.tool+" · event #"+selectedCall.seq:currentFile));
  const actions=el("div","context-actions");
  if(returnRoute&&activeView==="session")actions.append(button("← Return to "+workspaceRouteLabel(returnRoute),returnToWorkspace,"secondary"));
  else if(activeView==="session")actions.append(button("← Sessions",showLibrary,"secondary"));
  if(activeView!=="session")actions.append(button("Open evidence",async()=>{
    returnRoute=location.hash||"#sessions";
    setWorkspaceRoute(sessionHash(currentFile,inspectedEvent&&inspectedEvent!==selectedCall?.seq?"events":"activity",inspectedEvent||selectedCall?.seq));
    await restoreWorkspaceRoute();
  },"secondary"));
  actions.append(button("Save investigation",openInvestigationSave,"quiet"));
  out.append(label,actions);
  $("back").textContent=returnRoute?"← Return to "+workspaceRouteLabel(returnRoute):"← Session library";
}
function callActionBar(a) {
  const actions=el("div","call-actions");
  actions.append(button("Create regression case",createCaseFromCall,"primary"));
  actions.append(button("Compare recording",()=>showComparisons(currentFile),"secondary"));
  actions.append(button("Draft policy",()=>createPolicyProposal(),"secondary"));
  return actions;
}
function createCaseFromCall() {
  if(!selectedCall)return;
  showView("session");
  updateTestSelection();
  if(!$("lab-title-input").value.trim())$("lab-title-input").value=(selectedCall.tool+(selectedCall.status==="failed"?" error should not recur":" boundary expectation")).slice(0,200);
  if(!$("assertion-kind").value)$("assertion-kind").value=selectedCall.status==="failed"?"error_absent":selectedCall.verdict==="deny"?"denied":"answered";
  $("assertion-kind").onchange();
  setTab("test");
  $("lab-title-input").focus();
  $("test-panel").scrollIntoView({block:"start"});
}
function returnToWorkspace() {
  const target=returnRoute||"#sessions";
  returnRoute="";
  setWorkspaceRoute(target);
  restoreWorkspaceRoute();
}
function compareBaseFile() { return $("compare-base").value||currentFile; }
function syncComparisonPair() {
  for(const [id,value]of [["delta-reference",$("compare-base").value],["delta-current",$("compare-session").value]]) {
    const select=$(id);
    select.replaceChildren(new Option("Choose a recording",""));
    for(const r of workspaceRecordings)select.add(new Option(r.name||r.file,r.file));
    if([...select.options].some(o=>o.value===value))select.value=value;
  }
}
function updateComparisonCandidate() {
  const select=$("compare-session"),previous=select.value,base=$("compare-base").value;
  select.replaceChildren(new Option("Choose a recording to examine",""));
  for(const r of workspaceRecordings)if(r.file!==base)select.add(new Option(r.name+" · "+r.file,r.file));
  if([...select.options].some(o=>o.value===previous))select.value=previous;
  $("run-compare").disabled=single||!base||!select.value;
  syncComparisonPair();
}
async function showComparisons(reference="",candidate="",updateHash=true) {
  const request=++comparisonEpoch;
  pendingComparison={a:reference,b:candidate};
  showView("comparisons");
  $("breadcrumb").textContent="Workspace / Compare recordings";
  clearError();
  if(updateHash)setWorkspaceRoute("#compare"+(reference?"&a="+encodeURIComponent(reference):"")+(candidate?"&b="+encodeURIComponent(candidate):""));
  await loadCompareChoices();
  if(request===comparisonEpoch)pendingComparison=null;
}
async function runWorkspaceComparison() {
  const a=$("compare-base").value,b=$("compare-session").value;
  if(!a||!b||a===b) { notice($("compare-result"),"Choose two different recordings.","warning");return; }
  syncComparisonPair();
  setWorkspaceRoute("#compare&"+params({a,b}));
  $("compare-result").replaceChildren(el("p","muted","Comparing recorded calls…"));
  $("delta-result").replaceChildren(el("p","muted","Comparing behaviour…"));
  await Promise.all([compare(),runDelta()]);
}
function restoreLabRun(kind,file) {
  const saved=labRunCache[kind+":"+file];
  if(saved) {
    if(kind==="suite")renderSuiteResult(saved);else renderCaseResult(saved);
    $("lab-run").append(el("p","muted","Last run in this page session. Run again to check the current files."));
    if(location.hash.includes("&outcome=latest"))$("lab-run-title").scrollIntoView({block:"start"});
  } else {
    $("lab-run").replaceChildren();
    $("lab-run-target").textContent="No run outcome is cached for this "+kind+". Run it explicitly to check the current evidence.";
  }
}

// Bookmarks hold only navigation metadata, scoped to this recording directory.
// A malformed local-storage entry is data, never a route or script to execute.
const INVESTIGATION_KEY="tapelog-investigations-v1";
function readInvestigations() {
  try {
    const stored=JSON.parse(localStorage.getItem(INVESTIGATION_KEY)||"[]");
    if(!Array.isArray(stored))return [];
    return stored.slice(0,50).filter(x=>x&&typeof x.title==="string"&&x.title.length<=160&&typeof x.directory==="string"&&typeof x.file==="string"&&x.file.length<=512&&typeof x.route==="string"&&x.route.length<=4096&&x.route.startsWith("#session=")&&x.id===String(x.id));
  } catch { return []; }
}
function showInvestigations(updateHash=true) {
  showView("investigations");
  $("breadcrumb").textContent="Workspace / Investigations";
  if(updateHash)setWorkspaceRoute("#investigations");
  renderInvestigations();
}
function renderInvestigations() {
  const out=$("saved-investigations"),q=$("investigation-search").value.trim().toLowerCase();
  const entries=readInvestigations().filter(x=>x.directory===directory&&[x.title,x.file].join(" ").toLowerCase().includes(q));
  out.replaceChildren();
  $("save-investigation-page").disabled=!currentFile;
  if(!entries.length) {
    empty(out,q?"No matching investigations":"Keep the context, not a copy of the recording",q?"Try a shorter search.":"Open a session, select an event, then choose Save investigation. Your bookmark opens that place in the original evidence.");
    out.append(button("Browse sessions →",showLibrary,"primary"));
  }
  for(const item of entries) {
    const card=el("article","investigation-bookmark");
    const body=el("div","");
    const meta=el("p","investigation-meta");
    meta.append(el("span","when",when(item.saved)),el("span","mono",item.file+(item.seq?" · event #"+item.seq:"")));
    body.append(el("h2","",item.title),meta);
    card.append(body);
    const actions=el("div","actions");
    actions.append(button("Reopen investigation →",async()=> {
      returnRoute="#investigations";
      setWorkspaceRoute(item.route);
      await restoreWorkspaceRoute();
      if(item.revision&&currentInfo?.revision&&item.revision!==currentInfo.revision)notice($("error"),"This recording changed since the bookmark was saved. The current evidence is shown.","warning");
    },"primary"));
    const remove=button("Remove bookmark",()=> {
      try { localStorage.setItem(INVESTIGATION_KEY,JSON.stringify(readInvestigations().filter(x=>x.id!==item.id)));renderInvestigations(); }
      catch { error(new Error("Browser storage is unavailable; the bookmark was not removed.")); }
    },"quiet");
    remove.setAttribute("aria-label","Remove bookmark "+item.title);
    actions.append(remove);card.append(actions);out.append(card);
  }
}
function openInvestigationSave() {
  if(!currentFile)return;
  $("investigation-title").value=(currentInfo?.name||currentFile)+(selectedCall?" · "+selectedCall.tool:"");
  $("investigation-save-context").textContent=currentFile+(selectedCall?" · event #"+selectedCall.seq:"")+". Saved only in this browser, for this recording directory.";
  $("investigation-save-status").textContent="";
  $("investigation-dialog").showModal();
  $("investigation-title").focus();
}
function saveInvestigation(e) {
  e.preventDefault();
  const title=$("investigation-title").value.trim();
  if(!title||!currentFile)return;
  const seq=currentTab==="events"?inspectedEvent:selectedCall?.seq||0;
  const item={id:String(Date.now()),title:title.slice(0,160),directory,file:currentFile,seq,revision:currentInfo?.revision||"",route:sessionHash(currentFile,currentTab,seq),saved:new Date().toISOString()};
  try {
    localStorage.setItem(INVESTIGATION_KEY,JSON.stringify([item,...readInvestigations()].slice(0,50)));
    $("investigation-dialog").close();
    if(activeView==="investigations")renderInvestigations();
    else { const note=el("span","context-saved","Investigation saved");note.setAttribute("role","status");$("workspace-context").append(note); }
  } catch { $("investigation-save-status").textContent="Browser storage is unavailable. Nothing was saved. You can bookmark the current URL instead."; }
}

$("investigations-nav").onclick=()=>showInvestigations();
$("comparisons-nav").onclick=()=>showComparisons();
$("behaviour-to-compare").onclick=()=>showComparisons(currentFile);
$("library-nav").onclick=showLibrary;
$("back").onclick=()=>returnRoute?returnToWorkspace():showLibrary();
$("lab-nav").onclick=()=>{setLabTab("cases",false);showLab();};
$("suites-nav").onclick=()=>{setLabTab("suites",false);showLab();};
$("run-compare").onclick=runWorkspaceComparison;
$("compare-base").onchange=()=>{++compareRequest;++deltaRequest;$("compare-result").replaceChildren();$("delta-result").replaceChildren();updateComparisonCandidate();};
$("compare-session").onchange=()=>{++compareRequest;++deltaRequest;$("compare-result").replaceChildren();$("delta-result").replaceChildren();syncComparisonPair();$("run-compare").disabled=single||!$("compare-session").value;};
$("save-investigation-page").onclick=openInvestigationSave;
$("investigation-form").onsubmit=saveInvestigation;
$("investigation-cancel").onclick=()=>$("investigation-dialog").close();
$("investigation-search").oninput=()=>renderInvestigations();

const originalOpenEvidence=openBehaviourCall;
openBehaviourCall=async function(file,seq,index) {
  if(activeView!=="session")returnRoute=location.hash||"#sessions";
  await originalOpenEvidence(file,seq,index);
  updateWorkspaceContext();
};
const originalPolicyProposal=createPolicyProposal;
createPolicyProposal=async function() {
  const source=sessionHash(currentFile,currentTab,selectedCall?.seq);
  await originalPolicyProposal();
  if(activeView==="studio") {
    setWorkspaceRoute("#studio");
    const back=button("← Return to recorded call",async()=>{setWorkspaceRoute(source);await restoreWorkspaceRoute();},"secondary");
    $("builder-context").prepend(back);
    $("builder-context").scrollIntoView({block:"start"});
  }
};











/* --- Conversation-first investigation (M14) --------------------------
   A conversation is the unit a developer investigates, so it gets its own
   panel and its own deep link. Everything shown here comes from the server's
   analysis: the front end never re-derives membership, counts or order, and it
   never invents a conversation the recording did not declare. */
const CONVERSATION_ID=/^[A-Za-z0-9._:-]{1,128}$/;
let conversationView=null;
function validConversationId(id) {
  return typeof id==="string"&&CONVERSATION_ID.test(id);
}
function conversationRoute() {
  if(!location.hash.startsWith("#behaviour"))return null;
  const cut=location.hash.indexOf("&");
  if(cut<0)return null;
  const q=new URLSearchParams(location.hash.slice(cut+1));
  // #behaviour&a=…&b=… is the recording comparison, not a conversation.
  if(q.get("a")&&q.get("b"))return null;
  const id=q.get("conversation");
  if(!id)return null;
  return {
    id,compare:q.get("compare")||null
  };
}
function conversationHash(id,compare) {
  const q=new URLSearchParams({
    conversation:id
  });
  if(compare)q.set("compare",compare);
  return "#behaviour&"+q.toString();
}
function behaviourFilterQuery() {
  return {
    q:$("behaviour-search").value,server:$("behaviour-server").value,from:$("behaviour-from").value,to:$("behaviour-to").value
  };
}
function hideConversationPanel() {
  const panel=$("conversation-panel");
  if(!panel)return;
  panel.hidden=true;
  $("conversation-head").replaceChildren();
  $("conversation-detail").replaceChildren();
  $("conversation-compare-controls").replaceChildren();
  $("conversation-compare-result").replaceChildren();
  conversationView=null;
}
// applyConversationRoute opens whatever the hash names. A malformed or reserved
// id is refused here for the same reason the server refuses it: the
// unattributed remainder is not a conversation.
function applyConversationRoute() {
  const route=conversationRoute();
  if(!route) {
    hideConversationPanel();
    return null;
  }
  return openConversation(route.id,route.compare, {
    updateHash:false
  }
  );
}
async function openConversation(id,compare,opts) {
  const options=opts||{};
  const panel=$("conversation-panel");
  if(!panel)return;
  panel.hidden=false;
  if(!validConversationId(id)) {
    $("conversation-head").replaceChildren();
    // The comparison belonged to whatever was open before: leaving it above a
    // refusal would read as a result for the id in the link (M14 review F8).
    $("conversation-compare-result").replaceChildren();
    $("conversation-compare-controls").replaceChildren();
    const detail=$("conversation-detail");
    detail.replaceChildren();
    notice(detail,"That link does not name a conversation Tapelog can open. A conversation id is 1-128 characters of A-Z, a-z, 0-9, dot, underscore, colon or hyphen; the reserved unattributed remainder is not a conversation.","warning");
    return;
  }
  if(options.updateHash!==false)setWorkspaceRoute(conversationHash(id,compare));
  const limit=options.limit||100,offset=options.offset||0;
  conversationView= {
    id,compare:compare||null,offset,limit
  };
  $("conversation-head").replaceChildren(el("p","muted","Reading this conversation…"));
  $("conversation-detail").replaceChildren();
  $("conversation-compare-result").replaceChildren();
  renderConversationCompareControls(id,compare);
  try {
    const data=await api("/api/behaviour/conversation?"+params(Object.assign( {
      conversation:id,offset,limit
    }
    ,behaviourFilterQuery())));
    if(!conversationView||conversationView.id!==id)return;
    if(options.updateHash===false&&conversationView.offset!==offset)return;
    conversationView.data=data;
    renderConversationHead(data);
    renderConversationDetail(data);
    // The panel is the point of the click: bring it into view and put keyboard
    // focus on its heading instead of leaving the reader at the top of a long
    // library list.
    if(options.focus!==false) {
      panel.scrollIntoView( {
        block:"start"
      }
      );
      const heading=$("conversation-panel-title");
      if(heading) {
        heading.setAttribute("tabindex","-1");
        heading.focus();
      }
    }
    if(data.found&&compare)await runConversationComparison(id,compare);
  } catch(e) {
    notice($("conversation-detail"),"This conversation could not be read: "+e.message+". The recording may have been removed, renamed or filtered out; go back to the conversation list to choose another.","warning");
  }
}
function renderConversationHead(data) {
  const head=$("conversation-head");
  head.replaceChildren();
  if(!data.found) {
    head.append(el("div","banner warning","No analysed recording in this view declares this conversation id. It may have been renamed, removed, or filtered out by the current search or date range."));
    head.append(el("p","muted",data.reason||""));
    head.append(button("← Back to the conversation list",()=> {
      setWorkspaceRoute("#behaviour");
      hideConversationPanel();
    }
    ,"secondary"));
    return;
  }
  const line=el("div","conversation-open");
  line.append(el("code","conversation-id",conversationView.id),el("span","badge declared","client-declared"));
  head.append(line);
  const link=el("p","muted","This view is linkable: ");
  link.append(el("code","",conversationHash(conversationView.id,conversationView.compare)));
  link.append(el("span","",conversationView.compare?" — the link carries the conversation ids only, never arguments, results or tokens.":" — the link carries the conversation id only, never arguments, results or tokens."));
  head.append(link);
  const lib=data.library||  {
  };
  head.append(el("p","selection-note","Library coverage: "+(lib.analysed||0)+" of "+(lib.files||0)+" recordings analysed for this view"+(lib.truncated?" · the analysis budget was reached, so older recordings are not in this view":"")+". A conversation can appear in more than one of them."));
}
function renderConversationDetail(data) {
  const detail=$("conversation-detail");
  detail.replaceChildren();
  if(!data.found)return;
  const g=data.summary||  {
  };
  const counts=el("div","stat-strip conversation-stats");
  counts.append(statTile(g.calls,"Calls"),statTile(g.results,"Recorded results"),statTile(g.unanswered,"No recorded result"),statTile(g.denials,"Denied"),statTile(g.failures,"Tool errors"),statTile(g.ambiguous_calls||0,"Ambiguous calls"));
  detail.append(counts);
  const files=(g.files||[]);
  if(files.length) {
    const p=el("p","muted","Recordings: ");
    p.append(el("span","",files.join(" · ")));
    detail.append(p);
  }
  const tools=(g.tool_calls||[]);
  if(tools.length) {
    const ul=el("ul","occurrence-list conversation-tools");
    for(const t of tools) {
      const li=el("li");
      li.append(el("span","tool-name",t.tool),el("span","muted"," — "+(t.calls===1?"1 call":t.calls+" calls")));
      ul.append(li);
    }
    detail.append(ul);
  }
  for(const r of (g.repeats||[]))detail.append(el("p","muted","Inside this conversation, "+repeatSentence(r)));
  for(const note of (data.evidence||[]))detail.append(el("p","text-warn",note));
  const entries=data.timeline||[];
  if(!entries.length) {
    detail.append(el("p","muted","This conversation has no recorded calls in the current view."));
    return;
  }
  const multiFile=files.length>1;
  const list=el("ol","occurrence-list timeline");
  for(const e of entries)list.append(timelineItem(e,multiFile));
  detail.append(list);
  const total=data.total||entries.length;
  const shown=(data.offset||0)+entries.length;
  const pager=el("div","conversation-pager");
  const first=(data.offset||0)+1;
  pager.append(el("p","muted","Showing "+first+"–"+shown+" of "+total+" recorded call"+(total===1?"":"s")+", in library order. The #number is the recording's own event sequence, so gaps are policy decisions and results recorded between calls."));
  const actions=el("div","actions");
  if((data.offset||0)>0) {
    actions.append(button("← Previous page",()=>openConversation(data.conversation,conversationView&&conversationView.compare, {
      offset:Math.max(0,(data.offset||0)-(data.limit||100)),limit:data.limit||100
    }
    ),"secondary"));
  }
  if(data.has_more) {
    actions.append(button("Next page →",()=>openConversation(data.conversation,conversationView&&conversationView.compare, {
      offset:shown,limit:data.limit||100
    }
    ),"secondary"));
  }
  if(actions.children.length)pager.append(actions);
  detail.append(pager);
  // The last step of an investigation is a case that will hold next time. The
  // scope is carried into the builder so the saved case cannot quietly widen to
  // the whole recording.
  const next=el("div","actions conversation-actions");
  next.append(button("Draft a regression case for this conversation →",()=>draftConversationCase(data.conversation,entries[0]),"primary"));
  detail.append(next);
}
function statTile(value,label) {
  const tile=el("div","stat");
  tile.append(el("strong","",String(value===undefined||value===null?0:value)),el("span","",label));
  return tile;
}
function renderConversationCompareControls(id,compare) {
  const box=$("conversation-compare-controls");
  box.replaceChildren();
  const options=(behaviourData&&behaviourData.conversations?behaviourData.conversations:[]).filter(g=>g.attributed&&g.id!==id);
  if(!options.length) {
    box.append(el("p","muted","No other declared conversation is in this view, so there is nothing to compare with."));
    return;
  }
  const row=el("div","conversation-compare-picker");
  row.append(el("span","eyebrow","COMPARE WITH"));
  const select=el("select");
  select.setAttribute("aria-label","Conversation to compare with");
  select.append(new Option("Choose a conversation…",""));
  for(const g of options)select.append(new Option(g.id+(g.calls===1?" (1 call)":" ("+g.calls+" calls)"),g.id));
  if(compare&&options.some(g=>g.id===compare))select.value=compare;
  select.onchange=()=> {
    if(select.value) {
      openConversation(id,select.value);
    } else {
      setWorkspaceRoute(conversationHash(id));
      $("conversation-compare-result").replaceChildren();
    }
  };
  row.append(select);
  box.append(row);
}
async function runConversationComparison(a,b) {
  const out=$("conversation-compare-result");
  out.replaceChildren(el("p","muted","Comparing the two conversations…"));
  try {
    const data=await api("/api/behaviour/compare-conversations?"+params(Object.assign( {
      a,b
    }
    ,behaviourFilterQuery())));
    if(!conversationView||conversationView.id!==a)return;
    renderConversationComparison(out,data.comparison||  {
    });
  } catch(e) {
    notice(out,"The comparison could not be read: "+e.message,"warning");
  }
}
function renderConversationComparison(out,cmp) {
  out.replaceChildren();
  out.append(el("h3","","Comparison"));
  if(!cmp.comparable) {
    out.append(el("p","text-warn",cmp.explanation||"These two cannot be compared from this evidence."));
    return;
  }
  // The ids live in a caption line, not in the column headers: on a phone a
  // header cell holding "A · conv-alpha" cannot fit and overlaps its neighbours.
  const caption=el("p","muted comparison-caption");
  caption.append(el("strong","","A = "+(cmp.a?.id||"")),el("span",""," · "),el("strong","","B = "+(cmp.b?.id||"")));
  out.append(caption);
  const table=el("table","data-table comparison-table");
  const head=el("tr");
  head.append(el("th","","Metric"),el("th","","A"),el("th","","B"),el("th","","Δ (B−A)"));
  table.append(head);
  for(const m of (cmp.metrics||[])) {
    const tr=el("tr");
    tr.append(el("td","",m.metric),el("td","",String(m.a)),el("td","",String(m.b)),el("td","",(m.change>0?"+":"")+m.change));
    if(m.note)tr.setAttribute("title",m.note);
    table.append(tr);
  }
  out.append(table);
  out.append(el("p","muted","Change is B − A. These are counts of recorded boundary events, not scores."));
  if((cmp.findings||[]).length) {
    out.append(el("h4","","Observed differences"));
    const ul=el("ul","occurrence-list");
    for(const f of cmp.findings)ul.append(el("li","",f));
    out.append(ul);
  }
  if((cmp.tools||[]).length) {
    out.append(el("h4","","Tool usage"));
    const ul=el("ul","occurrence-list");
    for(const t of cmp.tools)ul.append(el("li","",t.tool+": "+t.a+" in A · "+t.b+" in B"));
    out.append(ul);
  }
  if((cmp.caveats||[]).length) {
    out.append(el("h4","","What this comparison cannot support"));
    for(const c of cmp.caveats)out.append(el("p","text-warn",c));
  }
}

// Draft a regression case scoped to one conversation from the conversation
// panel: the case builder opens on this conversation's first recorded call, and
// the scope field carries the id so the saved case cannot silently widen to the
// whole recording.
async function draftConversationCase(id, firstEntry) {
  if(!id||!firstEntry)return;
  await openBehaviourCall(firstEntry.file,firstEntry.seq,firstEntry.index||0);
  if(!selectedCall) {
    error(new Error("That conversation's call could not be opened, so the case builder has no evidence to point at."));
    return;
  }
  clearTest();
  $("lab-conversation").value=id;
  setTab("test");
  $("lab-title-input").value=("Conversation "+id+" boundary expectation").slice(0,200);
  $("test-panel").scrollIntoView({block:"start"});
}
