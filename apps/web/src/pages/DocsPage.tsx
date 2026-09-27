import React,{useMemo} from "react";
import {useToast} from "../providers/ToastProvider";

function CodeBlock({label,code}:{label:string;code:string}){
  const toast=useToast();

  async function copy(){
    try{
      await navigator.clipboard.writeText(code);
      toast.success("Copied to clipboard",label);
    }catch{
      toast.error("Copy failed","Select the code manually and copy it.");
    }
  }

  return <div className="docs-code">
    <div className="docs-code-head">
      <span>{label}</span>
      <button onClick={copy} aria-label={"Copy "+label}>Copy</button>
    </div>
    <pre><code>{code}</code></pre>
  </div>;
}

const errors=[
  ["400","Invalid JSON or unsupported parameter"],
  ["401","Invalid API key"],
  ["403","Key or model scope not permitted"],
  ["409","Idempotency conflict"],
  ["413","Payload too large"],
  ["422","Unsupported semantic combination"],
  ["429","Rate or concurrency limited"],
  ["502","Invalid or unusable upstream response"],
  ["503","No eligible route available"],
  ["504","Upstream timeout"],
];

export function DocsPage(){
  const baseUrl=useMemo(()=>window.location.origin+"/v1",[]);

  const curlModels=`curl ${baseUrl}/models \\\n  -H "Authorization: Bearer $NEXORA_API_KEY"`;

  const curlChat=`curl ${baseUrl}/chat/completions \\\n  -H "Authorization: Bearer $NEXORA_API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -H "Idempotency-Key: 550e8400-e29b-41d4-a716-446655440000" \\\n  -d '{"model":"auto-free","messages":[{"role":"user","content":"Hello from Nexora"}]}'`;

  const python=`from openai import OpenAI

client = OpenAI(
    base_url="${baseUrl}",
    api_key="nxa_live_..."
)

response = client.chat.completions.create(
    model="auto-free",
    messages=[
        {"role": "user", "content": "Hello from Nexora"}
    ]
)

print(response.choices[0].message.content)`;

  const javascript=`import OpenAI from "openai";

const client = new OpenAI({
  baseURL: "${baseUrl}",
  apiKey: "nxa_live_..."
});

const result = await client.chat.completions.create({
  model: "auto-free",
  messages: [{ role: "user", content: "Hello from Nexora" }]
});

console.log(result.choices[0].message.content);`;

  const streaming=`curl ${baseUrl}/chat/completions \\\n  -H "Authorization: Bearer $NEXORA_API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d '{"model":"auto-free","stream":true,"messages":[{"role":"user","content":"Stream a short answer"}]}'`;

  return <div className="docs-layout">
    <aside className="docs-toc" aria-label="Documentation sections">
      <strong>On this page</strong>
      <a href="#docs-quickstart">Quick start</a>
      <a href="#docs-auth">Authentication</a>
      <a href="#docs-models">Models</a>
      <a href="#docs-chat">Chat completions</a>
      <a href="#docs-sdks">SDK examples</a>
      <a href="#docs-streaming">Streaming</a>
      <a href="#docs-errors">Errors</a>
    </aside>

    <div className="docs-content">
      <section className="docs-hero" id="docs-quickstart">
        <span className="card-eyebrow">NEXORA API</span>
        <h2>OpenAI-compatible gateway</h2>
        <p>Use a Nexora API key and the public Nexora model IDs returned by <code>/v1/models</code>. Provider routing stays internal to the gateway.</p>
        <div className="docs-base-url">
          <span>Base URL</span>
          <code>{baseUrl}</code>
        </div>
      </section>

      <section className="docs-section" id="docs-auth">
        <div className="docs-section-head">
          <span className="docs-step">01</span>
          <div><h2>Authentication</h2><p>Send your Nexora API key as a Bearer token. Raw API keys are shown only when created or rotated.</p></div>
        </div>
        <CodeBlock label="List models with cURL" code={curlModels}/>
      </section>

      <section className="docs-section" id="docs-models">
        <div className="docs-section-head">
          <span className="docs-step">02</span>
          <div><h2>List models</h2><p><code>GET /v1/models</code> returns active free models using provider-neutral Nexora IDs.</p></div>
        </div>
        <div className="endpoint-card">
          <span className="http-method get">GET</span>
          <code>/v1/models</code>
          <p>Requires <code>Authorization: Bearer &lt;NEXORA_API_KEY&gt;</code>.</p>
        </div>
      </section>

      <section className="docs-section" id="docs-chat">
        <div className="docs-section-head">
          <span className="docs-step">03</span>
          <div><h2>Create a chat completion</h2><p><code>POST /v1/chat/completions</code> accepts OpenAI-compatible message requests. Use <code>auto-free</code> or a public model ID from the model catalog.</p></div>
        </div>
        <CodeBlock label="Chat completion with cURL" code={curlChat}/>
        <div className="docs-note">
          <strong>Idempotency</strong>
          <p><code>Idempotency-Key</code> is optional and supports up to 128 characters. Reusing the same key with a different request body returns HTTP 409.</p>
        </div>
      </section>

      <section className="docs-section" id="docs-sdks">
        <div className="docs-section-head">
          <span className="docs-step">04</span>
          <div><h2>OpenAI SDK examples</h2><p>Point OpenAI-compatible clients at the Nexora base URL and keep using the standard chat-completions interface.</p></div>
        </div>
        <div className="docs-example-grid">
          <CodeBlock label="Python" code={python}/>
          <CodeBlock label="JavaScript" code={javascript}/>
        </div>
      </section>

      <section className="docs-section" id="docs-streaming">
        <div className="docs-section-head">
          <span className="docs-step">05</span>
          <div><h2>Streaming</h2><p>Set <code>stream: true</code> to receive Server-Sent Events. Nexora preserves the OpenAI-compatible streaming shape and terminates completed streams with the standard done marker.</p></div>
        </div>
        <CodeBlock label="Streaming cURL" code={streaming}/>
      </section>

      <section className="docs-section" id="docs-errors">
        <div className="docs-section-head">
          <span className="docs-step">06</span>
          <div><h2>Error responses</h2><p>Use HTTP status codes to distinguish authentication, validation, rate-limit and provider-availability failures.</p></div>
        </div>
        <div className="docs-error-grid">
          {errors.map(([code,description])=><div className="docs-error-row" key={code}>
            <code>{code}</code>
            <span>{description}</span>
          </div>)}
        </div>
      </section>
    </div>
  </div>;
}
