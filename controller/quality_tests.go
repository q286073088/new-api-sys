package controller

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func qualityError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"success": false, "message": message})
}

func SaveQualityTest(c *gin.Context) {
	var test model.QualityTest
	if err := common.DecodeJson(c.Request.Body, &test); err != nil {
		qualityError(c, 400, "Invalid test configuration")
		return
	}
	test.ID = 0
	if c.Param("id") != "" {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			qualityError(c, 400, "Invalid test ID")
			return
		}
		test.ID = id
	}
	test.OwnerID = c.GetInt("id")
	test.Name = strings.TrimSpace(test.Name)
	if test.Name == "" || len(test.Name) > 200 || strings.TrimSpace(test.Prompt) == "" || len(test.Prompt) > 20000 || strings.TrimSpace(test.ExpectedAnswer) == "" || len(test.ExpectedAnswer) > 20000 || test.IntervalMinutes < 1 || test.IntervalMinutes > 10080 {
		qualityError(c, 400, "Provide a name, question, reference answer and an interval between 1 and 10080 minutes")
		return
	}
	if _, err := service.ValidateQualityTarget(test.QualityTarget); err != nil {
		qualityError(c, 400, err.Error())
		return
	}
	if test.Enabled {
		judge, err := model.GetQualityJudge()
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if _, err := service.ValidateQualityTarget(judge.QualityTarget); err != nil {
			qualityError(c, 400, "Configure a valid judge before enabling tests")
			return
		}
	}
	test.RequestedAt = 0
	test.NextRunAt = 0
	test.CreatedAt = 0
	test.UpdatedAt = 0
	if err := model.SaveQualityTest(&test); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"id": test.ID})
}

func DeleteQualityTest(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		qualityError(c, 400, "Invalid test ID")
		return
	}
	result := model.DB.Delete(&model.QualityTest{}, id)
	if result.Error != nil {
		common.ApiError(c, result.Error)
		return
	}
	if result.RowsAffected == 0 {
		qualityError(c, 404, "Test not found")
		return
	}
	common.ApiSuccess(c, nil)
}

func RunQualityTest(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		qualityError(c, 400, "Invalid test ID")
		return
	}
	judge, err := model.GetQualityJudge()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if _, err := service.ValidateQualityTarget(judge.QualityTarget); err != nil {
		qualityError(c, 400, "Configure a valid judge before running tests")
		return
	}
	if err := model.RequestQualityTest(id); err != nil {
		qualityError(c, 409, "Test is unavailable, queued or running")
		return
	}
	if _, _, err := service.EnqueueSystemTask(model.QualityTaskType, nil); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func GetQualityJudge(c *gin.Context) {
	judge, err := model.GetQualityJudge()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, judge)
}

func SaveQualityJudge(c *gin.Context) {
	var judge model.QualityJudge
	if err := common.DecodeJson(c.Request.Body, &judge); err != nil {
		qualityError(c, 400, "Invalid judge configuration")
		return
	}
	judge.OwnerID = c.GetInt("id")
	if len(judge.Instructions) > 20000 || strings.TrimSpace(judge.Instructions) == "" {
		qualityError(c, 400, "Provide judge instructions (up to 20000 bytes)")
		return
	}
	if _, err := service.ValidateQualityTarget(judge.QualityTarget); err != nil {
		qualityError(c, 400, err.Error())
		return
	}
	data, err := common.Marshal(judge)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// One option value is one atomic configuration snapshot. Read from DB on
	// execution so other masters never use a partially updated or stale target.
	option := model.Option{Key: model.QualityJudgeOption, Value: string(data)}
	err = model.DB.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"value"})}).Create(&option).Error
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func QualityTestOptions(c *gin.Context) {
	var tokens []model.Token
	if err := model.DB.Where("user_id = ?", c.GetInt("id")).Order("id desc").Find(&tokens).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	user, err := model.GetUserById(c.GetInt("id"), false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	options := make([]gin.H, 0, len(tokens))
	for _, token := range tokens {
		groups, err := service.QualityTokenGroups(&token, user.Group)
		if err != nil {
			continue
		}
		groupOptions := make([]gin.H, 0, len(groups))
		for _, group := range groups {
			models := model.GetGroupEnabledModels(group)
			if token.ModelLimitsEnabled {
				limits := token.GetModelLimitsMap()
				models = slices.DeleteFunc(slices.Clone(models), func(name string) bool { return !limits[name] })
			}
			groupOptions = append(groupOptions, gin.H{"name": group, "models": models})
		}
		options = append(options, gin.H{"id": token.Id, "name": token.Name, "key": token.GetMaskedKey(), "groups": groupOptions})
	}
	common.ApiSuccess(c, options)
}

func qualityVisibleTests(c *gin.Context) *gorm.DB {
	query := model.DB.Model(&model.QualityTest{})
	if !c.GetBool("quality_admin") {
		query = query.Where("public = ?", true)
	}
	return query
}

func ListQualityTests(c *gin.Context) {
	tests := []model.QualityTest{}
	if err := qualityVisibleTests(c).Order("id desc").Find(&tests).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var latest []model.QualityResult
	latestIDs := model.DB.Model(&model.QualityResult{}).Select("MAX(id)").Where("test_id IN (?)", qualityVisibleTests(c).Select("id")).Group("test_id")
	if err := model.DB.Select("test_id, status, started_at").Where("id IN (?)", latestIDs).Find(&latest).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	byTest := make(map[int64]model.QualityResult, len(latest))
	for _, result := range latest {
		byTest[result.TestID] = result
	}
	for i := range tests {
		tests[i].LatestStatus = byTest[tests[i].ID].Status
		tests[i].LatestAt = byTest[tests[i].ID].StartedAt
		// A manual run is queued before the worker creates its result row.
		// Surface that state immediately instead of showing an old result.
		if tests[i].RequestedAt > 0 {
			tests[i].LatestStatus = "running"
			tests[i].LatestAt = tests[i].RequestedAt
		}
	}
	if c.GetBool("quality_admin") {
		common.ApiSuccess(c, tests)
		return
	}
	public := make([]gin.H, 0, len(tests))
	for _, test := range tests {
		public = append(public, gin.H{"id": test.ID, "name": test.Name, "model": test.Model, "group": test.Group, "interval_minutes": test.IntervalMinutes, "enabled": test.Enabled, "next_run_at": test.NextRunAt, "latest_status": test.LatestStatus, "latest_at": test.LatestAt})
	}
	common.ApiSuccess(c, public)
}

// Apply visibility to every query, including direct record IDs, never just UI.
func qualityResultsQuery(c *gin.Context) (*gorm.DB, error) {
	query := model.DB.Model(&model.QualityResult{}).Where("test_id IN (?)", qualityVisibleTests(c).Select("id"))
	if value := c.Query("test_id"); value != "" {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 {
			return nil, errors.New("Invalid test ID")
		}
		query = query.Where("test_id = ?", id)
	}
	if name := c.Query("model"); name != "" {
		query = query.Where("model = ?", name)
	}
	if status := c.Query("status"); status != "" {
		if !slices.Contains([]string{"passed", "pending", "failed", "running"}, status) {
			return nil, errors.New("Invalid status")
		}
		query = query.Where("status = ?", status)
	}
	for _, field := range []string{"from", "to"} {
		if value := c.Query(field); value != "" {
			timestamp, err := strconv.ParseInt(value, 10, 64)
			if err != nil || timestamp < 0 {
				return nil, errors.New("Invalid time range")
			}
			if field == "from" {
				query = query.Where("started_at >= ?", timestamp)
			} else {
				query = query.Where("started_at < ?", timestamp)
			}
		}
	}
	return query, nil
}

func publicQualityResult(result *model.QualityResult) {
	result.Error = ""
	result.RequestID = ""
	result.JudgeRequestID = ""
	result.Snapshot = ""
}

func ListQualityResults(c *gin.Context) {
	query, err := qualityResultsQuery(c)
	if err != nil {
		qualityError(c, 400, err.Error())
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	page = max(1, min(page, 1000000))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	size = max(1, min(size, 100))
	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	results := []model.QualityResult{}
	if err := query.Omit("snapshot").Order("id desc").Offset((page - 1) * size).Limit(size).Find(&results).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if !c.GetBool("quality_admin") {
		for i := range results {
			publicQualityResult(&results[i])
		}
	}
	common.ApiSuccess(c, gin.H{"items": results, "total": total, "page": page, "page_size": size})
}

func GetQualityResult(c *gin.Context) {
	query, err := qualityResultsQuery(c)
	if err != nil {
		qualityError(c, 400, err.Error())
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		qualityError(c, 400, "Invalid result ID")
		return
	}
	var result model.QualityResult
	if err := query.Omit("snapshot").First(&result, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			qualityError(c, 404, "Result not found")
		} else {
			common.ApiError(c, err)
		}
		return
	}
	if !c.GetBool("quality_admin") {
		publicQualityResult(&result)
	}
	common.ApiSuccess(c, result)
}

type qualitySlot struct {
	Start  int64  `json:"start"`
	Status string `json:"status"`
	Count  int    `json:"count"`
}

func QualitySummary(c *gin.Context) {
	query, err := qualityResultsQuery(c)
	if err != nil {
		qualityError(c, 400, err.Error())
		return
	}
	now := time.Now().Unix()
	if c.Query("from") == "" && c.Query("to") == "" {
		query = query.Where("started_at >= ?", now-48*3600)
	}
	var counts []struct {
		Status string
		Count  int64
	}
	if err := query.Select("status, count(*) as count").Group("status").Scan(&counts).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	summary := map[string]int64{"passed": 0, "pending": 0, "failed": 0, "running": 0}
	for _, count := range counts {
		summary[count.Status] = count.Count
	}
	var next struct{ Next int64 }
	nextQuery := qualityVisibleTests(c).Where("enabled = ?", true)
	if c.Query("test_id") != "" {
		nextQuery = nextQuery.Where("id = ?", c.Query("test_id"))
	}
	if c.Query("model") != "" {
		nextQuery = nextQuery.Where("model = ?", c.Query("model"))
	}
	if err := nextQuery.Select("COALESCE(MIN(next_run_at), 0) as next").Scan(&next).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	// Always show a bounded rolling 48-hour timeline. History filters affect the
	// counts/table; the timeline independently reflects the actual last 48 hours.
	const timelineBucketSeconds int64 = 3600
	const timelineBucketCount = 48
	end := (now/timelineBucketSeconds + 1) * timelineBucketSeconds
	start := end - timelineBucketSeconds*timelineBucketCount
	timelineQuery := model.DB.Model(&model.QualityResult{}).Where("test_id IN (?)", qualityVisibleTests(c).Select("id")).Where("started_at >= ? AND started_at < ?", start, end)
	if c.Query("test_id") != "" {
		timelineQuery = timelineQuery.Where("test_id = ?", c.Query("test_id"))
	}
	if c.Query("model") != "" {
		timelineQuery = timelineQuery.Where("model = ?", c.Query("model"))
	}
	var points []struct {
		TestID    int64
		Status    string
		StartedAt int64
	}
	if err := timelineQuery.Select("test_id, status, started_at").Find(&points).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	timeline := map[int64][]qualitySlot{}
	priority := map[string]int{"": 0, "passed": 1, "running": 2, "pending": 3, "failed": 4}
	for _, point := range points {
		if timeline[point.TestID] == nil {
			slots := make([]qualitySlot, timelineBucketCount)
			for i := range slots {
				slots[i].Start = start + int64(i)*timelineBucketSeconds
			}
			timeline[point.TestID] = slots
		}
		i := (point.StartedAt - start) / timelineBucketSeconds
		slot := &timeline[point.TestID][i]
		slot.Count++
		if priority[point.Status] > priority[slot.Status] {
			slot.Status = point.Status
		}
	}
	common.ApiSuccess(c, gin.H{"counts": summary, "next_run_at": next.Next, "timeline": timeline, "timeline_start": start, "server_time": now})
}

func QualityAdminContext(c *gin.Context) { c.Set("quality_admin", true); c.Next() }

// Disabling execution or hiding results must work even after credentials/models
// become unavailable. Enabling is validated separately from editing the target.
func UpdateQualityTestFlags(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		qualityError(c, 400, "Invalid test ID")
		return
	}
	var flags struct {
		Enabled *bool `json:"enabled"`
		Public  *bool `json:"public"`
	}
	if err := common.DecodeJson(c.Request.Body, &flags); err != nil || (flags.Enabled == nil && flags.Public == nil) {
		qualityError(c, 400, "Invalid test flags")
		return
	}
	var test model.QualityTest
	if err := model.DB.First(&test, id).Error; err != nil {
		qualityError(c, 404, "Test not found")
		return
	}
	if flags.Enabled != nil && *flags.Enabled {
		if _, err := service.ValidateQualityTarget(test.QualityTarget); err != nil {
			qualityError(c, 400, err.Error())
			return
		}
		judge, err := model.GetQualityJudge()
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if _, err := service.ValidateQualityTarget(judge.QualityTarget); err != nil {
			qualityError(c, 400, "Configure a valid judge before enabling tests")
			return
		}
	}
	if err := model.UpdateQualityTestFlags(id, flags.Enabled, flags.Public); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
