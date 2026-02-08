# Conversation Storage

KoukeBOT 支援兩種對話儲存方式：

## FileStore（預設）

持久化儲存，將對話儲存為 JSON 檔案。

### 檔案位置
- **路徑**：`data/conversations/`
- **檔案格式**：`{channelID}_{personaID}.json`
- **範例**：`data/conversations/web-test_hayasaka_silver_beater.json`

### 特性
- ✅ **持久化**：重啟 server 後對話記錄保留
- ✅ **人類可讀**：JSON 格式，可直接查看和編輯
- ✅ **自動建立**：目錄不存在時自動建立
- ✅ **檔案名稱安全**：自動清理特殊字元

### JSON 結構範例
```json
{
  "id": "web-test:hayasaka_silver_beater",
  "channel_id": "web-test",
  "persona_id": "hayasaka_silver_beater",
  "messages": [
    {
      "role": "user",
      "content": "hi",
      "timestamp": "2026-02-08T18:44:01Z"
    },
    {
      "role": "assistant",
      "content": "...",
      "timestamp": "2026-02-08T18:44:05Z"
    }
  ],
  "created_at": "2026-02-08T18:44:01Z",
  "updated_at": "2026-02-08T18:44:05Z"
}
```

### 使用方式
```go
// In cmd/character/main.go
convStore, err := store.NewFileStore("data/conversations")
```

---

## MemoryStore（開發用）

純記憶體儲存，適合快速測試。

### 特性
- ⚡ **速度快**：無 I/O 操作
- 🧪 **測試友善**：無需清理檔案
- ⚠️ **不持久**：重啟後資料清空

### 使用方式
```go
// 替換 FileStore 為 MemoryStore
convStore := store.NewMemoryStore()
```

---

## 資料隱私

- **gitignore**：`data/` 目錄已被排除，不會被 commit
- **權限**：對話檔案權限為 `0644`（僅 owner 可寫）
- **本地儲存**：對話僅儲存在本機，不會上傳至任何雲端服務

---

## 進階功能（未來）

可參考 [openclaw](https://github.com/openclaw/openclaw) 實作：

- **JSONL 格式**：支援 streaming append（每行一個訊息）
- **壓縮**：長時間對話自動壓縮
- **匯出**：匯出對話為 Markdown/PDF
- **搜尋**：全文搜尋歷史對話
- **統計**：Token 使用量、對話長度統計
