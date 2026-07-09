### Task 6: 配置示例与文档

在 `config.example.yaml` 加示例配置项，方便自用。

**Files:**
- Modify: `config.example.yaml`

- [ ] **Step 1: 加示例**

在 `config.example.yaml` 末尾（或 `openai-compatibility` 段附近）追加：

```yaml
# Vision recognition model for OpenAI-compatible providers.
# When a user sends an image to a text-only model via an openai-compatibility
# provider, the image is sent to this vision model for recognition, and the
# resulting text summary is fed back to the original text model.
# Format: "<provider-name>/<model-name>" where provider-name matches an
# openai-compatibility entry below.
# vision-recognition-model: "openai-official/gpt-4o"
```

- [ ] **Step 2: 提交**

```bash
cd D:/Dev/Spock/Relay/CliRelay
git add config.example.yaml
git commit -m "docs: 添加 vision-recognition-model 配置示例"
```

---

## Self-Review 结果

**1. Spec 覆盖：**
- §5 配置 → Task 3
- §6.1 视觉模型判断 → Task 1
- §6.2 识图调用器 → Task 2
- §6.3 图片替换 → Task 4（复用 `ReplaceImagePartEx`）
- §7.1 Execute 接入 → Task 5 Step 5
- §7.2 ExecuteStream 接入 → Task 5 Step 6
- §7.3 错误处理 → Task 4 `recognizeOneImage` 降级、Task 4 测试覆盖
- §8 usage 记录 → Task 5 接入 `contextWithVisionFallbackLog`
- §9 测试 → 各 Task 均含 TDD 步骤 + Task 5 端到端
- §10 影响范围 → 文件结构与各 Task 一致

**2. 类型一致性：** `SupportsVisionByModelName`、`NewOpenAICompatAnalyzer`、`ResolveRecognitionTarget`、`RecognitionTarget`、`RecognizeCurrentTurnImages`、`RecognizeImagesResult` 在各 Task 中签名一致。

**3. 已知风险点（实现时关注）：**
- `ImageSourceKindInline` 常量名需核对 types.go
- 远程 URL 图片本版本不专门处理，降级为失败占位符
- Task 5 端到端测试若被翻译层影响，降级为直接流程测试
