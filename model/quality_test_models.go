package model

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const QualityJudgeOption = "quality_test.judge"
const QualityTaskType = "quality_test"

// QualityTarget contains only a reference to a local token, never its secret.
type QualityTarget struct {
	TokenID  int    `json:"token_id"`
	OwnerID  int    `json:"owner_id"`
	Model    string `json:"model" gorm:"size:255"`
	Group    string `json:"group" gorm:"column:group_name;size:255"`
	Endpoint string `json:"endpoint" gorm:"size:32"`
}

type QualityJudge struct {
	QualityTarget
	Instructions string `json:"instructions"`
}

type QualityTest struct {
	LatestStatus    string `json:"latest_status" gorm:"-:all"`
	LatestAt        int64  `json:"latest_at" gorm:"-:all"`
	ID              int64  `json:"id" gorm:"primaryKey"`
	Name            string `json:"name" gorm:"size:200"`
	QualityTarget   `gorm:"embedded"`
	Prompt          string         `json:"prompt" gorm:"type:text"`
	ExpectedAnswer  string         `json:"expected_answer" gorm:"type:text"`
	IntervalMinutes int            `json:"interval_minutes"`
	Enabled         bool           `json:"enabled"`
	Public          bool           `json:"public"`
	NextRunAt       int64          `json:"next_run_at" gorm:"index"`
	RequestedAt     int64          `json:"requested_at"`
	CreatedAt       int64          `json:"created_at"`
	UpdatedAt       int64          `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `json:"-" gorm:"index"`
}

type QualityResult struct {
	BatchTaskID    string `json:"-" gorm:"size:64;index"`
	RunnerID       string `json:"-" gorm:"-"`
	ID             int64  `json:"id" gorm:"primaryKey"`
	TestID         int64  `json:"test_id" gorm:"index:idx_quality_history,priority:1"`
	Name           string `json:"name" gorm:"size:200"`
	Model          string `json:"model" gorm:"size:255"`
	Group          string `json:"group" gorm:"column:group_name;size:255"`
	Prompt         string `json:"prompt" gorm:"type:text"`
	ExpectedAnswer string `json:"expected_answer" gorm:"type:text"`
	Answer         string `json:"answer" gorm:"type:text"`
	Status         string `json:"status" gorm:"size:32;index"`
	Verdict        string `json:"verdict" gorm:"size:32"`
	Reason         string `json:"reason" gorm:"type:text"`
	FailureStage   string `json:"failure_stage" gorm:"size:32"`
	Error          string `json:"error,omitempty" gorm:"type:text"`
	StartedAt      int64  `json:"started_at" gorm:"index:idx_quality_history,priority:2"`
	FinishedAt     int64  `json:"finished_at"`
	DurationMS     int64  `json:"duration_ms"`
	RequestID      string `json:"request_id" gorm:"size:128"`
	JudgeRequestID string `json:"judge_request_id" gorm:"size:128"`
	Snapshot       string `json:"-" gorm:"size:1048576"`
}

type QualitySnapshot struct {
	Test  QualityTest  `json:"test"`
	Judge QualityJudge `json:"judge"`
}

func GetQualityJudge() (QualityJudge, error) {
	judge := QualityJudge{Instructions: "Compare the answer with the reference answer in the context of the question. Accept semantically equivalent correct answers. Explain the decision briefly.", QualityTarget: QualityTarget{Endpoint: "chat"}}
	var option Option
	err := DB.Where(&Option{Key: QualityJudgeOption}).First(&option).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return judge, nil
	}
	if err != nil {
		return judge, err
	}
	err = common.UnmarshalJsonStr(option.Value, &judge)
	return judge, err
}

// ClaimQualityTest serializes edits/manual requests with scheduled execution.
// The system task's renewable lease additionally fences the worker batch.
func ClaimQualityTest(id int64, judge QualityJudge, now int64, batchTaskID, runnerID string) (*QualityResult, error) {
	var result *QualityResult
	err := DB.Transaction(func(tx *gorm.DB) error {
		var lease SystemTaskLock
		if err := lockForUpdate(tx).Where("task_id = ? AND locked_by = ? AND locked_until >= ?", batchTaskID, runnerID, now).First(&lease).Error; err != nil {
			return ErrSystemTaskLockLost
		}
		var test QualityTest
		if err := lockForUpdate(tx).First(&test, id).Error; err != nil {
			return err
		}
		if test.RequestedAt == 0 && (!test.Enabled || test.NextRunAt > now) {
			return nil
		}
		var running int64
		if err := tx.Model(&QualityResult{}).Where("test_id = ? AND status = ?", id, "running").Count(&running).Error; err != nil {
			return err
		}
		if running > 0 {
			return nil
		}
		manual := test.RequestedAt != 0
		changes := map[string]any{"requested_at": 0}
		if !manual {
			changes["next_run_at"] = now + int64(test.IntervalMinutes)*60
		}
		if err := tx.Model(&test).Updates(changes).Error; err != nil {
			return err
		}
		snapshot, err := common.Marshal(QualitySnapshot{Test: test, Judge: judge})
		if err != nil {
			return err
		}
		result = &QualityResult{BatchTaskID: batchTaskID, RunnerID: runnerID, TestID: id, Name: test.Name, Model: test.Model, Group: test.Group, Prompt: test.Prompt, ExpectedAnswer: test.ExpectedAnswer, Status: "running", StartedAt: now, Snapshot: string(snapshot)}
		return tx.Create(result).Error
	})
	return result, err
}

func CheckQualityLease(result *QualityResult) error {
	var count int64
	err := DB.Model(&SystemTaskLock{}).Where("task_id = ? AND locked_by = ? AND locked_until >= ?", result.BatchTaskID, result.RunnerID, time.Now().Unix()).Count(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrSystemTaskLockLost
	}
	return nil
}

func FinishQualityResult(result *QualityResult) error {
	result.FinishedAt = time.Now().Unix()
	return DB.Model(&QualityResult{}).Where("id = ? AND status = ?", result.ID, "running").
		Where("EXISTS (?)", DB.Model(&SystemTaskLock{}).Select("1").Where("task_id = ? AND locked_by = ? AND locked_until >= ?", result.BatchTaskID, result.RunnerID, time.Now().Unix())).Updates(map[string]any{
		"answer": result.Answer, "status": result.Status, "verdict": result.Verdict, "reason": result.Reason,
		"failure_stage": result.FailureStage, "error": result.Error, "finished_at": result.FinishedAt,
		"duration_ms": result.DurationMS, "request_id": result.RequestID, "judge_request_id": result.JudgeRequestID,
	}).Error
}

func SaveQualityTest(test *QualityTest) error {
	now := time.Now().Unix()
	return DB.Transaction(func(tx *gorm.DB) error {
		if test.ID == 0 {
			test.CreatedAt = now
			test.UpdatedAt = now
			if test.Enabled {
				// A freshly created enabled test runs once immediately, then
				// follows its schedule. First-time setup stays observable
				// instead of staying silent until the next interval elapses.
				test.RequestedAt = now
				test.NextRunAt = now + int64(test.IntervalMinutes)*60
			}
			return tx.Create(test).Error
		}
		var old QualityTest
		if err := lockForUpdate(tx).First(&old, test.ID).Error; err != nil {
			return err
		}
		next := old.NextRunAt
		if !test.Enabled {
			next = 0
		} else if !old.Enabled || old.IntervalMinutes != test.IntervalMinutes {
			next = now + int64(test.IntervalMinutes)*60
		}
		updates := map[string]any{"name": test.Name, "token_id": test.TokenID, "owner_id": test.OwnerID, "model": test.Model, "group_name": test.Group, "endpoint": test.Endpoint, "prompt": test.Prompt, "expected_answer": test.ExpectedAnswer, "interval_minutes": test.IntervalMinutes, "enabled": test.Enabled, "public": test.Public, "next_run_at": next, "updated_at": now}
		if test.Enabled && !old.Enabled {
			// Enabling a paused test also runs it once immediately.
			updates["requested_at"] = now
		}
		return tx.Model(&old).Updates(updates).Error
	})
}

func RequestQualityTest(id int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var test QualityTest
		if err := lockForUpdate(tx).First(&test, id).Error; err != nil {
			return err
		}
		var running int64
		if err := tx.Model(&QualityResult{}).Where("test_id = ? AND status = ?", id, "running").Count(&running).Error; err != nil {
			return err
		}
		if running > 0 || test.RequestedAt > 0 {
			return errors.New("Test is already queued or running")
		}
		return tx.Model(&test).Update("requested_at", time.Now().Unix()).Error
	})
}

func UpdateQualityTestFlags(id int64, enabled, public *bool) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var test QualityTest
		if err := lockForUpdate(tx).First(&test, id).Error; err != nil {
			return err
		}
		updates := map[string]any{"updated_at": time.Now().Unix()}
		if enabled != nil {
			updates["enabled"] = *enabled
			if !*enabled {
				updates["next_run_at"] = 0
			} else if !test.Enabled {
				// Enabling runs the test once immediately, then on schedule.
				updates["next_run_at"] = time.Now().Unix() + int64(test.IntervalMinutes)*60
				updates["requested_at"] = time.Now().Unix()
			}
		}
		if public != nil {
			updates["public"] = *public
		}
		return tx.Model(&test).Updates(updates).Error
	})
}
