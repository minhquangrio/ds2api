package videos

import (
	"strconv"
	"strings"

	"ds2api/internal/mediacompat"
)

// renderTask projects a job record onto the OpenAI video object shape. Fields
// DS2API adds (stage, media availability) are explicitly namespaced so a client
// written against the documented schema ignores them safely.
func renderTask(task mediacompat.VideoTask, requestedModel string) map[string]any {
	model := strings.TrimSpace(task.Model)
	if model == "" {
		model = requestedModel
	}
	body := map[string]any{
		"id":         task.ID,
		"object":     "video",
		"model":      model,
		"status":     task.Status,
		"progress":   task.Progress,
		"created_at": int64(task.CreatedAt),
	}
	if task.Status == "completed" && task.UpdatedAt > 0 {
		body["completed_at"] = int64(task.UpdatedAt)
	}
	if task.DurationSeconds > 0 {
		body["seconds"] = strconv.Itoa(task.DurationSeconds)
	}
	if task.Error != "" {
		body["error"] = map[string]any{"message": task.Error, "code": task.ErrorCode}
	}
	if strings.TrimSpace(task.MediaID) != "" {
		body["has_content"] = true
		body["content_url"] = "/v1/videos/" + task.ID + "/content"
	}
	if stage := strings.TrimSpace(task.Stage); stage != "" {
		body["stage"] = stage
	}
	return body
}

func renderTasks(tasks []mediacompat.VideoTask) []map[string]any {
	body := make([]map[string]any, 0, len(tasks))
	for _, task := range tasks {
		body = append(body, renderTask(task, ""))
	}
	return body
}

func videoExtension(contentType string) string {
	declared := strings.ToLower(contentType)
	switch {
	case strings.Contains(declared, "webm"):
		return "webm"
	case strings.Contains(declared, "quicktime"), strings.Contains(declared, "mov"):
		return "mov"
	default:
		return "mp4"
	}
}
