import "./HowItWorks.css";

export default function HowItWorks() {
  return (
    <div className="how-page">
      <h2>How KoukeBOT Works</h2>
      <p className="how-intro">
        這頁整理目前 KoukeBOT 的對話資料流、帳號池容錯、Token 節流策略，以及 OAuth 新增帳號流程。
      </p>

      <section className="how-section">
        <h3>1) 對話資料流</h3>
        <div className="how-flow">
          <pre>{`User Message (Discord / Web)
  -> Discord Handler / API Handler
  -> 組裝上下文（history + replied message + current message）
  -> ChatService
  -> ContextAssembler（System + Digest + Recent）
  -> ProviderPool（選帳號、容錯重試）
  -> LLM 回覆串流
  -> 回寫 Conversation + 回傳 Discord/Web`}</pre>
        </div>
        <ol className="how-list">
          <li>群組訊息會抓頻道歷史（預算驅動，預設目標 12 筆）。</li>
          <li>如果是 Discord reply，會額外把被回覆訊息當成高優先 context 注入。</li>
          <li>被回覆訊息若含圖片，最多補 2 張，避免 context 爆量。</li>
        </ol>
      </section>

      <section className="how-section">
        <h3>2) 帳號池容錯演算法</h3>
        <div className="how-grid">
          <article className="how-card">
            <h4>選路策略</h4>
            <p>可用帳號優先，再看健康度（連續失敗較少者優先），最後做輕量輪轉。</p>
          </article>
          <article className="how-card">
            <h4>錯誤分類</h4>
            <p>
              quota(429)、validation、transient(5xx/網路/timeout)、other。quota/transient 會切換帳號，validation 直接返回。
            </p>
          </article>
          <article className="how-card">
            <h4>短延遲重試</h4>
            <p>quota reset 很短時採 exponential backoff + jitter；失敗再切下一帳號。</p>
          </article>
          <article className="how-card">
            <h4>健康指標</h4>
            <p>每帳號維護 consecutive_failures、last_error、last_success_at、request_count。</p>
          </article>
        </div>
      </section>

      <section className="how-section">
        <h3>3) Token 節流（分層記憶）</h3>
        <div className="how-code">
          <code>historyBudget = contextWindow - responseReserve - systemPromptTokens</code>
        </div>
        <ol className="how-list">
          <li>Thinking 模型採三層：System Prompt + Relevant Digests + Recent Window。</li>
          <li>Digest 由關鍵詞重疊 + recency 計分，挑最相關摘要回填。</li>
          <li>工具輸出（web_fetch/web_search/file_read）過長會先壓縮再回灌。</li>
          <li>長對話壓縮時，舊訊息會轉為結構化 digest（facts/preferences/open items/decisions）。</li>
        </ol>
      </section>

      <section className="how-section">
        <h3>4) OAuth 新增帳號（本機 + 遠端）</h3>
        <ol className="how-list">
          <li>
            <code>POST /api/oauth/gemini/start</code> 回傳 <code>flow_id</code>、<code>auth_url</code>、<code>expires_at</code>、
            <code>mode</code>。
          </li>
          <li>前端開 popup 做 Google 授權。</li>
          <li>
            成功可走 callback，或遠端模式用 <code>POST /api/oauth/gemini/complete</code> 手動完成。
          </li>
          <li>完成後統一路徑：refresh token → discover project → AddOrReplace pool → persist pool。</li>
        </ol>
      </section>

      <section className="how-section">
        <h3>5) 觀察與除錯資料</h3>
        <div className="how-grid">
          <article className="how-card">
            <h4>Pool 狀態</h4>
            <p>
              透過 <code>/api/oauth/pool</code> 可看到帳號可用性、配額、健康欄位，快速定位哪個帳號在失敗。
            </p>
          </article>
          <article className="how-card">
            <h4>Conversation Digests</h4>
            <p>
              conversation JSON 內新增 <code>context_digests</code>，可回溯壓縮後記憶內容是否合理。
            </p>
          </article>
        </div>
      </section>
    </div>
  );
}

