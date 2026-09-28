import React,{KeyboardEvent,useEffect,useMemo,useRef,useState} from "react";
import {
  api,
  ConsoleModel,
  PlaygroundConversation,
  PlaygroundMessage,
  PlaygroundSession,
} from "../api";

function formatWhen(value:string){
  const date=new Date(value);
  if(Number.isNaN(date.getTime()))return "";
  const diff=Math.max(0,Date.now()-date.getTime());
  const minutes=Math.floor(diff/60000);
  if(minutes<1)return "now";
  if(minutes<60)return `${minutes}m`;
  const hours=Math.floor(minutes/60);
  if(hours<24)return `${hours}h`;
  const days=Math.floor(hours/24);
  if(days<7)return `${days}d`;
  return date.toLocaleDateString(undefined,{month:"short",day:"numeric"});
}

function totalTokens(message:PlaygroundMessage){
  if(message.prompt_tokens===null&&message.completion_tokens===null)return null;
  return (message.prompt_tokens??0)+(message.completion_tokens??0);
}

function modelLabel(message:PlaygroundMessage,models:ConsoleModel[]){
  if(message.model_display_name)return message.model_display_name;
  if(message.model_id){
    const model=models.find(item=>item.id===message.model_id);
    if(model)return model.display_name;
  }
  return message.model_public_id||"Nexora model";
}

export function PlaygroundPage({
  projectId,
  models,
  initialModelPublicId,
}:{
  projectId:string;
  models:ConsoleModel[];
  initialModelPublicId:string;
}){
  const [sessions,setSessions]=useState<PlaygroundSession[]>([]);
  const [activeSessionId,setActiveSessionId]=useState("");
  const [messages,setMessages]=useState<PlaygroundMessage[]>([]);
  const [selectedModelId,setSelectedModelId]=useState("");
  const [input,setInput]=useState("");
  const [search,setSearch]=useState("");
  const [loadingSessions,setLoadingSessions]=useState(false);
  const [loadingConversation,setLoadingConversation]=useState(false);
  const [sending,setSending]=useState(false);
  const [error,setError]=useState("");
  const endRef=useRef<HTMLDivElement|null>(null);
  const abortRef=useRef<AbortController|null>(null);

  const defaultModel=useMemo(
    ()=>models.find(item=>item.public_id===initialModelPublicId)
      ||models.find(item=>item.public_id==="auto-free")
      ||models[0]
      ||null,
    [models,initialModelPublicId],
  );

  const modelMap=useMemo(()=>new Map(models.map(item=>[item.id,item])),[models]);
  const selectedModel=selectedModelId?modelMap.get(selectedModelId)||null:null;
  const filteredSessions=useMemo(()=>{
    const query=search.trim().toLowerCase();
    if(!query)return sessions;
    return sessions.filter(item=>
      item.title.toLowerCase().includes(query)
      ||(item.preview||"").toLowerCase().includes(query)
    );
  },[sessions,search]);

  useEffect(()=>{
    if(!selectedModelId||!modelMap.has(selectedModelId)){
      setSelectedModelId(defaultModel?.id||"");
    }
  },[defaultModel,modelMap,selectedModelId]);

  useEffect(()=>{
    let cancelled=false;
    setMessages([]);
    setActiveSessionId("");
    setError("");
    if(!projectId){
      setSessions([]);
      return()=>{cancelled=true};
    }
    setLoadingSessions(true);
    api.playgroundSessions(projectId)
      .then(rows=>{
        if(cancelled)return;
        setSessions(rows);
        if(rows.length)setActiveSessionId(rows[0].id);
      })
      .catch(err=>{if(!cancelled)setError(err instanceof Error?err.message:String(err))})
      .finally(()=>{if(!cancelled)setLoadingSessions(false)});
    return()=>{cancelled=true};
  },[projectId]);

  useEffect(()=>{
    let cancelled=false;
    if(!activeSessionId){
      setMessages([]);
      if(defaultModel)setSelectedModelId(defaultModel.id);
      return()=>{cancelled=true};
    }
    setLoadingConversation(true);setError("");
    api.playgroundSession(activeSessionId)
      .then((conversation:PlaygroundConversation)=>{
        if(cancelled)return;
        setMessages(conversation.messages);
        if(conversation.session.selected_model_id){
          setSelectedModelId(conversation.session.selected_model_id);
        }
      })
      .catch(err=>{if(!cancelled)setError(err instanceof Error?err.message:String(err))})
      .finally(()=>{if(!cancelled)setLoadingConversation(false)});
    return()=>{cancelled=true};
  },[activeSessionId,defaultModel]);

  useEffect(()=>{
    endRef.current?.scrollIntoView({behavior:sending?"smooth":"auto",block:"end"});
  },[messages,sending]);

  function newChat(){
    setActiveSessionId("");
    setMessages([]);
    setInput("");
    setError("");
    setSelectedModelId(defaultModel?.id||"");
  }

  async function openSession(id:string){
    if(id===activeSessionId)return;
    setActiveSessionId(id);
    setError("");
  }

  async function removeSession(id:string){
    const target=sessions.find(item=>item.id===id);
    if(!window.confirm(`Delete "${target?.title||"this chat"}"? This removes its Playground history.`))return;
    try{
      await api.deletePlaygroundSession(id);
      setSessions(current=>current.filter(item=>item.id!==id));
      if(activeSessionId===id)newChat();
    }catch(err){
      setError(err instanceof Error?err.message:String(err));
    }
  }

  function stop(){
    abortRef.current?.abort();
  }

  async function send(){
    const prompt=input.trim();
    if(!prompt||!projectId||!selectedModelId||sending)return;

    setSending(true);setError("");setInput("");
    let sessionId=activeSessionId;
    const controller=new AbortController();
    abortRef.current=controller;

    try{
      if(!sessionId){
        const created=await api.createPlaygroundSession(projectId,selectedModelId);
        sessionId=created.id;
        setSessions(current=>[created,...current.filter(item=>item.id!==created.id)]);
      }

      const resolvedSessionId=sessionId;
      const stamp=Date.now();
      const optimisticUser:PlaygroundMessage={
        id:`temp-user-${stamp}`,
        session_id:resolvedSessionId,
        role:"user",
        content:prompt,
        model_id:selectedModelId,
        model_public_id:selectedModel?.public_id||null,
        model_display_name:selectedModel?.display_name||null,
        request_id:null,
        status:null,
        ttft_ms:null,
        latency_ms:null,
        prompt_tokens:null,
        completion_tokens:null,
        created_at:new Date().toISOString(),
      };
      const optimisticAssistant:PlaygroundMessage={
        ...optimisticUser,
        id:`temp-assistant-${stamp}`,
        role:"assistant",
        content:"",
        created_at:new Date(Date.now()+1).toISOString(),
      };
      setMessages(current=>[...current,optimisticUser,optimisticAssistant]);

      await api.streamPlaygroundMessage(
        resolvedSessionId,
        prompt,
        selectedModelId,
        {
          onDelta:delta=>{
            setMessages(current=>current.map(item=>
              item.id===optimisticAssistant.id
                ?{...item,content:item.content+delta}
                :item
            ));
          },
        },
        controller.signal,
      );

      const refreshed=await api.playgroundSession(resolvedSessionId);
      setMessages(refreshed.messages);
      setActiveSessionId(refreshed.session.id);
      setSessions(current=>[
        refreshed.session,
        ...current.filter(item=>item.id!==refreshed.session.id),
      ]);
    }catch(err){
      const aborted=controller.signal.aborted;
      if(!aborted)setError(err instanceof Error?err.message:String(err));
      else setError("Generation stopped before completion.");

      if(sessionId){
        setActiveSessionId(sessionId);
        try{
          const refreshed=await api.playgroundSession(sessionId);
          setMessages(refreshed.messages);
          setSessions(current=>[
            refreshed.session,
            ...current.filter(item=>item.id!==refreshed.session.id),
          ]);
        }catch{
          // Preserve the original streaming error when history refresh fails.
        }
      }
    }finally{
      if(abortRef.current===controller)abortRef.current=null;
      setSending(false);
    }
  }

  function onComposerKeyDown(event:KeyboardEvent<HTMLTextAreaElement>){
    if(event.key==="Enter"&&!event.shiftKey){
      event.preventDefault();
      void send();
    }
  }

  if(!projectId){
    return <section className="playground-chat-shell playground-empty-project">
      <div className="playground-empty-state">
        <div className="playground-empty-mark">N</div>
        <h2>Select a project</h2>
        <p>Playground conversations are isolated by project.</p>
      </div>
    </section>;
  }

  return <div className="playground-chat-shell">
    <aside className="playground-history" aria-label="Playground chat history">
      <div className="playground-history-head">
        <div>
          <span className="card-eyebrow">Playground</span>
          <strong>Test conversations</strong>
        </div>
        <button className="playground-new-chat" onClick={newChat} disabled={sending}>+ New</button>
      </div>

      <div className="playground-history-search">
        <span aria-hidden="true">⌕</span>
        <input
          aria-label="Search Playground history"
          value={search}
          onChange={event=>setSearch(event.target.value)}
          placeholder="Search chats"
        />
      </div>

      <div className="playground-history-list">
        {loadingSessions?<div className="playground-history-state"><span className="spinner"/>Loading chats…</div>
        :filteredSessions.length?filteredSessions.map(item=><div
          key={item.id}
          className={"playground-history-item "+(item.id===activeSessionId?"active":"")}
        >
          <button
            className="playground-history-open"
            onClick={()=>void openSession(item.id)}
            disabled={sending}
            aria-current={item.id===activeSessionId?"page":undefined}
          >
            <span className="playground-history-copy">
              <strong>{item.title}</strong>
              <small>{item.preview||"No messages yet"}</small>
            </span>
            <time>{formatWhen(item.updated_at)}</time>
          </button>
          <button
            className="playground-delete-chat"
            aria-label={`Delete ${item.title}`}
            onClick={()=>void removeSession(item.id)}
            disabled={sending}
          >×</button>
        </div>):<div className="playground-history-state">No saved chats yet.</div>}
      </div>

      <div className="playground-history-foot">
        <span className="environment-dot"/>
        Session authenticated
      </div>
    </aside>

    <section className="playground-chat">
      <header className="playground-chat-head">
        <div className="playground-model-picker">
          <label htmlFor="playground-model">Model</label>
          <select
            id="playground-model"
            value={selectedModelId}
            onChange={event=>setSelectedModelId(event.target.value)}
            disabled={sending||models.length===0}
          >
            {models.map(item=><option key={item.id} value={item.id}>
              {item.display_name}{item.public_id==="auto-free"?" · automatic":""}
            </option>)}
          </select>
        </div>
        <div className="playground-head-meta">
          <span className="status active">free models</span>
          <span>No API key required</span>
        </div>
      </header>

      <div className="playground-thread" aria-live="polite">
        {loadingConversation?<div className="playground-thread-loading"><span className="spinner"/>Loading conversation…</div>
        :messages.length===0&&!sending?<div className="playground-empty-state">
          <div className="playground-empty-mark">N</div>
          <h2>What do you want to test?</h2>
          <p>Select a free model and start a conversation. Nexora keeps this Playground history inside your current project.</p>
        </div>:<div className="playground-message-list">
          {messages.map(message=><article className={"playground-message "+message.role} key={message.id}>
            <div className="playground-message-avatar">{message.role==="assistant"?"N":"Y"}</div>
            <div className="playground-message-body">
              <div className="playground-message-author">
                <strong>{message.role==="assistant"?"Nexora":"You"}</strong>
                {message.role==="assistant"&&<span>{modelLabel(message,models)}</span>}
              </div>
              <div className="playground-message-content">
                {message.content}
                {message.role==="assistant"&&message.id.startsWith("temp-assistant-")&&sending&&<span className="playground-stream-cursor" aria-hidden="true"/>}
                {message.role==="assistant"&&message.id.startsWith("temp-assistant-")&&!message.content&&<span className="playground-typing" role="status"><i/><i/><i/></span>}
              </div>
              {message.role==="assistant"&&message.status!==null&&<div className="playground-message-metrics">
                <span className={(message.status>=200&&message.status<300)?"metric-ok":"metric-error"}>
                  <i/>{message.status}
                </span>
                {message.latency_ms!==null&&<span>{(message.latency_ms/1000).toFixed(2)}s response</span>}
                {message.ttft_ms!==null&&<span>{message.ttft_ms}ms TTFT</span>}
                {message.prompt_tokens!==null&&<span>{message.prompt_tokens} in</span>}
                {message.completion_tokens!==null&&<span>{message.completion_tokens} out</span>}
                {totalTokens(message)!==null&&<span>{totalTokens(message)} tokens</span>}
              </div>}
            </div>
          </article>)}
          <div ref={endRef}/>
        </div>}
      </div>

      <footer className="playground-composer-wrap">
        {error&&<div className="playground-inline-error" role="alert"><span>!</span>{error}</div>}
        {models.length===0&&<div className="playground-inline-error" role="status"><span>!</span>No active free models are available yet.</div>}
        <div className="playground-composer">
          <textarea
            value={input}
            onChange={event=>setInput(event.target.value)}
            onKeyDown={onComposerKeyDown}
            placeholder="Ask Nexora anything…"
            rows={1}
            maxLength={16000}
            disabled={sending||models.length===0}
            aria-label="Playground message"
          />
          <button
            className={"playground-send "+(sending?"stop":"")}
            onClick={sending?stop:()=>void send()}
            disabled={!sending&&(!input.trim()||!selectedModelId)}
            aria-label={sending?"Stop generation":"Send message"}
          >{sending?"■":"↑"}</button>
        </div>
        <div className="playground-composer-meta">
          <span>Enter to send · Shift+Enter for new line</span>
          <span>{selectedModel?.display_name||"Select a model"}</span>
        </div>
      </footer>
    </section>
  </div>;
}
