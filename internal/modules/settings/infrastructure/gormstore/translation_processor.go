package settingsstore

import (
	"context"
	"fmt"
	"strings"
	"time"

	openaitranslate "github.com/dujiao-next/internal/modules/settings/infrastructure/openai"
	settingsintegration "github.com/dujiao-next/internal/modules/settings/schema/integration"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/google/uuid"
)

// TranslationJobProcessor 翻译任务处理器
type TranslationJobProcessor struct {
	store    *Store
	client   openaitranslate.Client
	settings translationSettingGetter
}

type translationSettingGetter interface {
	GetTranslationSetting() (settingsintegration.TranslationSetting, error)
}

func NewTranslationJobProcessor(
	store *Store,
	client openaitranslate.Client,
	settings translationSettingGetter,
) *TranslationJobProcessor {
	return &TranslationJobProcessor{
		store:    store,
		client:   client,
		settings: settings,
	}
}

// SubmitJob 提交翻译任务
func (p *TranslationJobProcessor) SubmitJob(ctx context.Context, fields map[string]string) (string, error) {
	fieldsMap := make(jsonmap.JSON, len(fields))
	for key, text := range fields {
		fieldsMap[key] = text
	}

	job := &TranslationJobRecord{
		ID:        uuid.New().String(),
		Status:    TranslationJobStatusPending,
		Fields:    fieldsMap,
		Progress:  0,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := p.store.CreateTranslationJob(job); err != nil {
		return "", fmt.Errorf("create job: %w", err)
	}

	// 启动后台处理
	go p.processJobAsync(job.ID)

	return job.ID, nil
}

// GetJobStatus 查询任务状态
func (p *TranslationJobProcessor) GetJobStatus(ctx context.Context, jobID string) (interface{}, error) {
	job, err := p.store.GetTranslationJob(jobID)
	if err != nil {
		return nil, fmt.Errorf("get job: %w", err)
	}

	// 将存储层的 TranslationJobRecord 转换为 API 响应格式
	response := map[string]interface{}{
		"id":         job.ID,
		"status":     job.Status,
		"progress":   job.Progress,
		"created_at": job.CreatedAt,
		"updated_at": job.UpdatedAt,
	}

	if job.Status == TranslationJobStatusCompleted && job.Result != nil {
		response["result"] = job.Result
	}

	if job.Status == TranslationJobStatusFailed && job.Error != "" {
		response["error"] = job.Error
	}

	return response, nil
}

// processJobAsync 异步处理翻译任务（串行处理所有分组）
func (p *TranslationJobProcessor) processJobAsync(jobID string) {
	ctx := context.Background()

	job, err := p.store.GetTranslationJob(jobID)
	if err != nil {
		return
	}

	// 更新为处理中
	job.Status = TranslationJobStatusProcessing
	job.UpdatedAt = time.Now()
	_ = p.store.UpdateTranslationJob(job)

	// 解析待翻译字段
	fields := make(map[string]string, len(job.Fields))
	for key, value := range job.Fields {
		text, ok := value.(string)
		if !ok {
			continue
		}
		fields[key] = text
	}

	// 获取翻译配置
	setting, err := p.settings.GetTranslationSetting()
	if err != nil {
		p.failJob(job, fmt.Errorf("get setting: %w", err))
		return
	}

	if !setting.Enabled || setting.APIKey == "" {
		p.failJob(job, fmt.Errorf("translation not configured"))
		return
	}

	// 按长度分组：>1500 字沿 HTML 安全边界切片；300-1500 字单独请求；≤300 字批量请求
	const longFieldThreshold = 300
	const chunkThreshold = 1500 // 超过此长度需要切片
	const chunkSize = 800       // 每个切片的目标字符数
	longFields := make(map[string]string)
	shortFields := make(map[string]string)
	chunkedFields := make(map[string][]string) // 需要切片的超长字段

	for key, text := range fields {
		if text == "" {
			continue
		}
		runeCount := len([]rune(text))

		if runeCount > chunkThreshold {
			chunkedFields[key] = splitHTMLSafe(text, chunkSize)
		} else if runeCount > longFieldThreshold {
			longFields[key] = text
		} else {
			shortFields[key] = text
		}
	}

	// 计算总组数
	totalGroups := len(longFields) + len(chunkedFields)
	for _, chunks := range chunkedFields {
		totalGroups += len(chunks) - 1 // 每个切片字段的多个chunk
	}
	if len(shortFields) > 0 {
		totalGroups++
	}
	completedGroups := 0
	result := make(map[string]map[string]string)

	// 串行处理短字段批量请求（如果有）
	if len(shortFields) > 0 {
		items := make([]openaitranslate.Item, 0, len(shortFields))
		for key, text := range shortFields {
			items = append(items, openaitranslate.Item{Key: key, Text: text})
		}

		groupResult, err := p.client.Translate(ctx, setting, items)
		if err != nil {
			p.failJob(job, fmt.Errorf("translate short fields: %w", err))
			return
		}

		for key, translations := range groupResult {
			result[key] = translations
		}

		completedGroups++
		job.Progress = (completedGroups * 100) / totalGroups
		job.UpdatedAt = time.Now()
		_ = p.store.UpdateTranslationJob(job)
	}

	// 串行处理长字段（每个单独请求）
	for key, text := range longFields {
		items := []openaitranslate.Item{{Key: key, Text: text}}

		groupResult, err := p.client.Translate(ctx, setting, items)
		if err != nil {
			p.failJob(job, fmt.Errorf("translate field %s: %w", key, err))
			return
		}

		for k, translations := range groupResult {
			result[k] = translations
		}

		completedGroups++
		job.Progress = (completedGroups * 100) / totalGroups
		job.UpdatedAt = time.Now()
		_ = p.store.UpdateTranslationJob(job)
	}

	// 串行处理超长切片字段（每个切片单独请求，按原始顺序拼接）
	for key, chunks := range chunkedFields {
		zhTWParts := make([]string, len(chunks))
		enUSParts := make([]string, len(chunks))

		for i, chunk := range chunks {
			chunkKey := fmt.Sprintf("%s_chunk_%d", key, i)
			items := []openaitranslate.Item{{Key: chunkKey, Text: chunk}}

			groupResult, err := p.client.Translate(ctx, setting, items)
			if err != nil {
				p.failJob(job, fmt.Errorf("translate field %s chunk %d: %w", key, i, err))
				return
			}

			if translations, ok := groupResult[chunkKey]; ok {
				zhTWParts[i] = translations["zh-TW"]
				enUSParts[i] = translations["en-US"]
			}

			completedGroups++
			job.Progress = (completedGroups * 100) / totalGroups
			job.UpdatedAt = time.Now()
			_ = p.store.UpdateTranslationJob(job)
		}

		result[key] = map[string]string{
			"zh-TW": strings.Join(zhTWParts, ""),
			"en-US": strings.Join(enUSParts, ""),
		}
	}

	// 完成任务
	resultMap := make(jsonmap.JSON, len(result))
	for key, translations := range result {
		resultMap[key] = translations
	}

	job.Status = TranslationJobStatusCompleted
	job.Result = resultMap
	job.Progress = 100
	job.UpdatedAt = time.Now()
	_ = p.store.UpdateTranslationJob(job)
}

func (p *TranslationJobProcessor) failJob(job *TranslationJobRecord, err error) {
	job.Status = TranslationJobStatusFailed
	job.Error = err.Error()
	job.UpdatedAt = time.Now()
	_ = p.store.UpdateTranslationJob(job)
}

// htmlSafeBreakpoints 是切片时优先寻找的边界标记，按优先级从高到低排列：
// 块级标签闭合处最安全，其次是句末标点，最后才退化到任意空白。
// 在这些位置切开不会把一个 HTML 标签（如 <strong>）从中间切断，
// 避免模型收到破损标签后把它当作普通文本照抄，导致输出里出现字面的
// 转义序列（例如 <strong>）而不是真正被渲染的标签。
var htmlSafeBreakpoints = []string{
	"</p>", "</li>", "</ul>", "</ol>", "</div>", "<br>", "<br/>", "<br />",
	"。", "！", "？", "\n\n", "\n",
}

// splitHTMLSafe 把 text 切成若干段，每段长度尽量接近 targetSize（按 rune 计数），
// 且切点必须落在 htmlSafeBreakpoints 之一的结束位置，避免切断 HTML 标签或转义实体。
func splitHTMLSafe(text string, targetSize int) []string {
	runes := []rune(text)
	total := len(runes)
	if total <= targetSize {
		return []string{text}
	}

	var chunks []string
	start := 0
	for start < total {
		end := start + targetSize
		if end >= total {
			chunks = append(chunks, string(runes[start:total]))
			break
		}

		cut := findSafeBreak(runes, start, end)
		if cut <= start {
			// 找不到安全边界（例如一大段没有标点的连续文本），
			// 退化为硬切，但仍优先保证不落在多字节符文或 HTML 转义实体中间。
			cut = end
		}
		chunks = append(chunks, string(runes[start:cut]))
		start = cut
	}
	return chunks
}

// findSafeBreak 在 [searchStart, limit] 范围内从后往前找最靠近 limit 的安全切点。
// 找不到时返回 -1，调用方据此退化为硬切。
func findSafeBreak(runes []rune, searchStart, limit int) int {
	window := string(runes[searchStart:limit])
	best := -1
	for _, marker := range htmlSafeBreakpoints {
		if idx := strings.LastIndex(window, marker); idx >= 0 {
			cut := searchStart + len([]rune(window[:idx])) + len([]rune(marker))
			if cut > best {
				best = cut
			}
		}
	}
	if best <= searchStart {
		return -1
	}
	return best
}
