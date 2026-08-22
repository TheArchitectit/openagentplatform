package bridge

import (
	"time"

	"github.com/openagentplatform/openagentplatform/a2a/models"
)

// publishUpdate publishes a status update derived from the current task state.
func (rb *RPCBridge) publishUpdate(task *models.Task) {
	if task == nil {
		return
	}
	rb.gw.Hub().Publish(task.ID, models.TaskStatusUpdate{
		TaskID:    task.ID,
		Status:    task.Status,
		UpdatedAt: task.UpdatedAt,
	})
}

// publishUpdateRaw publishes a status update with a custom status and message.
func (rb *RPCBridge) publishUpdateRaw(taskID, status, message string) {
	rb.gw.Hub().Publish(taskID, models.TaskStatusUpdate{
		TaskID:    taskID,
		Status:    status,
		Message:   message,
		UpdatedAt: time.Now().UTC(),
	})
}

// messageToParts converts an A2A models.Message into the bridge Part slice
// that the Python adapter service expects.
func messageToParts(msg models.Message) []Part {
	if len(msg.Parts) == 0 {
		return nil
	}
	parts := make([]Part, 0, len(msg.Parts))
	for _, p := range msg.Parts {
		bp := Part{Type: "text", Text: p.Text}
		if p.File != nil {
			bp.Type = "file"
			bp.FileURL = p.File.URI
			bp.FileMIME = p.File.MimeType
		}
		parts = append(parts, bp)
	}
	return parts
}

// partsToModelsParts converts bridge Parts back into a2a models.Parts.
func partsToModelsParts(parts []Part) []models.Part {
	result := make([]models.Part, 0, len(parts))
	for _, p := range parts {
		mp := models.Part{Text: p.Text}
		if p.Type == "file" && p.FileURL != "" {
			mp.File = &models.FileRef{
				URI:      p.FileURL,
				MimeType: p.FileMIME,
			}
		}
		result = append(result, mp)
	}
	return result
}
