package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
)

type taskManager struct {
	store  *taskStore
	logger *slog.Logger
}

func newTaskManager(store *taskStore, logger *slog.Logger) *taskManager {
	return &taskManager{store: store, logger: logger}
}

func (m *taskManager) Enqueue(taskType string, payload any, runner func(context.Context) (any, error)) (*schemas.TaskResponse, error) {
	taskID, err := newTaskID()
	if err != nil {
		return nil, err
	}
	record, err := m.store.newRecord(taskID, taskType, payload)
	if err != nil {
		return nil, err
	}
	if err := m.store.create(context.Background(), record); err != nil {
		return nil, err
	}

	response := recordToResponse(record, nil)
	if runner == nil {
		return response, nil
	}

	go func() {
		startTime := time.Now().UTC().Format(time.RFC3339Nano)
		update := taskRecord{ID: taskID, Status: schemas.TaskStatusRunning, StartedAt: startTime}
		if err := m.store.update(context.Background(), update); err != nil {
			m.logger.Error("Failed to mark task running", "task_id", taskID, "error", err)
		}

		result, runErr := runner(context.Background())
		finishTime := time.Now().UTC().Format(time.RFC3339Nano)
		resultJSON, err := m.store.marshalResult(result)
		if err != nil {
			m.logger.Error("Failed to encode task result", "task_id", taskID, "error", err)
		}

		finalUpdate := taskRecord{ID: taskID, FinishedAt: finishTime, ResultJSON: resultJSON}
		if runErr != nil {
			finalUpdate.Status = schemas.TaskStatusFailed
			finalUpdate.Error = runErr.Error()
		} else {
			finalUpdate.Status = schemas.TaskStatusSucceeded
		}
		if err := m.store.update(context.Background(), finalUpdate); err != nil {
			m.logger.Error("Failed to finalize task", "task_id", taskID, "error", err)
		}
	}()

	return response, nil
}

func (m *taskManager) Get(taskID string) (*schemas.TaskResponse, error) {
	record, err := m.store.get(context.Background(), taskID)
	if err != nil {
		return nil, err
	}
	result, err := decodeTaskResult(record.ResultJSON)
	if err != nil {
		return nil, err
	}
	return recordToResponse(*record, result), nil
}

func recordToResponse(record taskRecord, result *schemas.TaskResult) *schemas.TaskResponse {
	return &schemas.TaskResponse{
		TaskID:     record.ID,
		Type:       record.Type,
		Status:     record.Status,
		CreatedAt:  record.CreatedAt,
		StartedAt:  record.StartedAt,
		FinishedAt: record.FinishedAt,
		Error:      record.Error,
		Result:     result,
	}
}

func decodeTaskResult(resultJSON string) (*schemas.TaskResult, error) {
	if resultJSON == "" {
		return nil, nil
	}
	var result schemas.TaskResult
	if err := jsonUnmarshal(resultJSON, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func newTaskID() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func jsonUnmarshal(data string, target any) error {
	if data == "" {
		return errors.New("empty json")
	}
	return json.Unmarshal([]byte(data), target)
}
