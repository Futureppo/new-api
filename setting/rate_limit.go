package setting

import (
	"fmt"
	"math"
	"strconv"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

var ModelRequestRateLimitEnabled = false
var ModelRequestRateLimitDurationMinutes = 1
var ModelRequestRateLimitCount = 0
var ModelRequestRateLimitSuccessCount = 1000
var ModelRequestConcurrencyLimit = 2
var ModelRequestRateLimitGroup = map[string][]int{}
var ModelRequestRateLimitMutex sync.RWMutex

func ModelRequestRateLimitGroup2JSONString() string {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	jsonBytes, err := common.Marshal(ModelRequestRateLimitGroup)
	if err != nil {
		common.SysLog("error marshalling model ratio: " + err.Error())
	}
	return string(jsonBytes)
}

func UpdateModelRequestRateLimitGroupByJSONString(jsonStr string) error {
	rateLimitGroup, err := parseModelRequestRateLimitGroup(jsonStr)
	if err != nil {
		return err
	}

	ModelRequestRateLimitMutex.Lock()
	defer ModelRequestRateLimitMutex.Unlock()

	ModelRequestRateLimitGroup = rateLimitGroup
	return nil
}

func GetGroupRateLimit(group string) (totalCount, successCount, concurrencyLimit int, hasConcurrencyLimit, found bool) {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	if ModelRequestRateLimitGroup == nil {
		return 0, 0, 0, false, false
	}

	limits, found := ModelRequestRateLimitGroup[group]
	if !found || len(limits) < 2 {
		return 0, 0, 0, false, false
	}
	if len(limits) >= 3 {
		return limits[0], limits[1], limits[2], true, true
	}
	return limits[0], limits[1], 0, false, true
}

func CheckModelRequestRateLimitGroup(jsonStr string) error {
	_, err := parseModelRequestRateLimitGroup(jsonStr)
	return err
}

func CheckModelRequestConcurrencyLimit(value string) error {
	limit, err := strconv.Atoi(value)
	if err != nil {
		return err
	}
	if limit < 0 {
		return fmt.Errorf("model request concurrency limit must be greater than or equal to 0")
	}
	if limit > math.MaxInt32 {
		return fmt.Errorf("model request concurrency limit has max value 2147483647")
	}
	return nil
}

func parseModelRequestRateLimitGroup(jsonStr string) (map[string][]int, error) {
	rateLimitGroup := make(map[string][]int)
	err := common.UnmarshalJsonStr(jsonStr, &rateLimitGroup)
	if err != nil {
		return nil, err
	}
	for group, limits := range rateLimitGroup {
		if len(limits) < 2 || len(limits) > 3 {
			return nil, fmt.Errorf("group %s rate limit config must contain 2 or 3 values", group)
		}
		if limits[0] < 0 || limits[1] < 1 {
			return nil, fmt.Errorf("group %s has invalid rate limit values: [%d, %d]", group, limits[0], limits[1])
		}
		if limits[0] > math.MaxInt32 || limits[1] > math.MaxInt32 {
			return nil, fmt.Errorf("group %s [%d, %d] has max rate limits value 2147483647", group, limits[0], limits[1])
		}
		if len(limits) == 3 {
			if limits[2] < 0 {
				return nil, fmt.Errorf("group %s has invalid concurrency limit value: %d", group, limits[2])
			}
			if limits[2] > math.MaxInt32 {
				return nil, fmt.Errorf("group %s concurrency limit has max value 2147483647", group)
			}
		}
	}
	return rateLimitGroup, nil
}
