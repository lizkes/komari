package clients

import (
	"fmt"
	"time"

	"github.com/komari-monitor/komari/common"
	"github.com/komari-monitor/komari/database/dbcore"
	"github.com/komari-monitor/komari/database/models"
	"github.com/komari-monitor/komari/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/google/uuid"
)

// Deprecated: DeleteClientConfig is deprecated and will be removed in a future release. Use DeleteClient instead.
func DeleteClientConfig(clientUuid string) error {
	db := dbcore.GetDBInstance()
	err := db.Delete(&common.ClientConfig{ClientUUID: clientUuid}).Error
	if err != nil {
		return err
	}
	return nil
}
func DeleteClient(clientUuid string) error {
	db := dbcore.GetDBInstance()
	err := db.Delete(&models.Client{}, "uuid = ?", clientUuid).Error
	if err != nil {
		return err
	}
	return nil
}

// Deprecated: UpdateOrInsertBasicInfo is deprecated and will be removed in a future release. Use SaveClientInfo instead.
func UpdateOrInsertBasicInfo(cbi common.ClientInfo) error {
	db := dbcore.GetDBInstance()
	updates := make(map[string]interface{})

	if cbi.Name != "" {
		updates["name"] = cbi.Name
	}
	if cbi.CpuName != "" {
		updates["cpu_name"] = cbi.CpuName
	}
	if cbi.Arch != "" {
		updates["arch"] = cbi.Arch
	}
	if cbi.CpuCores != 0 {
		updates["cpu_cores"] = cbi.CpuCores
	}
	if cbi.OS != "" {
		updates["os"] = cbi.OS
	}
	if cbi.GpuName != "" {
		updates["gpu_name"] = cbi.GpuName
	}
	if cbi.IPv4 != "" {
		updates["ipv4"] = cbi.IPv4
	}
	if cbi.IPv6 != "" {
		updates["ipv6"] = cbi.IPv6
	}
	if cbi.Region != "" {
		updates["region"] = cbi.Region
	}
	if cbi.Remark != "" {
		updates["remark"] = cbi.Remark
	}
	updates["mem_total"] = cbi.MemTotal
	updates["swap_total"] = cbi.SwapTotal
	updates["disk_total"] = cbi.DiskTotal
	updates["version"] = cbi.Version
	updates["updated_at"] = time.Now()

	// 转换为更新Client表
	client := models.Client{
		UUID: cbi.UUID,
	}

	err := db.Model(&client).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "uuid"}},
		DoUpdates: clause.Assignments(updates),
	}).Create(map[string]interface{}{
		"uuid":       cbi.UUID,
		"name":       cbi.Name,
		"cpu_name":   cbi.CpuName,
		"arch":       cbi.Arch,
		"cpu_cores":  cbi.CpuCores,
		"os":         cbi.OS,
		"gpu_name":   cbi.GpuName,
		"ipv4":       cbi.IPv4,
		"ipv6":       cbi.IPv6,
		"region":     cbi.Region,
		"remark":     cbi.Remark,
		"mem_total":  cbi.MemTotal,
		"swap_total": cbi.SwapTotal,
		"disk_total": cbi.DiskTotal,
		"version":    cbi.Version,
		"updated_at": time.Now(),
	}).Error

	if err != nil {
		return err
	}
	return nil
}
func SaveClientInfo(update map[string]interface{}) error {
	utils.LogDebug("Database: SaveClientInfo called with data: %+v", update)

	db := dbcore.GetDBInstance()
	clientUUID, ok := update["uuid"].(string)
	if !ok || clientUUID == "" {
		utils.LogDebug("Database: Invalid or missing client UUID in update data")
		return fmt.Errorf("invalid client UUID")
	}

	utils.LogDebug("Database: Processing update for client UUID: %s", clientUUID)

	// 确保更新的字段不为空
	if len(update) == 0 {
		utils.LogDebug("Database: No fields to update for client %s", clientUUID)
		return fmt.Errorf("no fields to update")
	}

	// 记录具体要更新的字段
	utils.LogDebug("Database: Fields to update: %d", len(update))
	for key, value := range update {
		utils.LogDebug("Database: Field '%s' = '%v' (type: %T)", key, value, value)
	}

	update["updated_at"] = time.Now()
	utils.LogDebug("Database: Added updated_at timestamp")

	// 执行数据库更新前先检查当前记录
	var currentClient models.Client
	if err := db.Where("uuid = ?", clientUUID).First(&currentClient).Error; err != nil {
		utils.LogDebug("Database: Failed to find current client record: %v", err)
		return fmt.Errorf("client not found: %v", err)
	}
	utils.LogDebug("Database: Current client record before update: %+v", currentClient)

	// 执行更新操作
	utils.LogDebug("Database: Executing GORM Updates() operation...")
	result := db.Model(&models.Client{}).Where("uuid = ?", clientUUID).Updates(update)

	if result.Error != nil {
		utils.LogDebug("Database: GORM Updates() failed: %v", result.Error)
		return result.Error
	}

	utils.LogDebug("Database: GORM Updates() completed - RowsAffected: %d", result.RowsAffected)

	// 检查更新后的记录
	var updatedClient models.Client
	if err := db.Where("uuid = ?", clientUUID).First(&updatedClient).Error; err != nil {
		utils.LogDebug("Database: Failed to retrieve updated client record: %v", err)
	} else {
		utils.LogDebug("Database: Updated client record after save: %+v", updatedClient)
	}

	utils.LogDebug("Database: SaveClientInfo completed successfully for UUID: %s", clientUUID)
	return nil
}

// 更新客户端设置
func UpdateClientConfig(config common.ClientConfig) error {
	db := dbcore.GetDBInstance()
	err := db.Save(&config).Error
	if err != nil {
		return err
	}
	return nil
}

func EditClientName(clientUUID, clientName string) error {
	db := dbcore.GetDBInstance()
	err := db.Model(&models.Client{}).Where("uuid = ?", clientUUID).Update("name", clientName).Error
	if err != nil {
		return err
	}
	return nil
}

/*
// UpdateClientByUUID 更新指定 UUID 的客户端配置

	func UpdateClientByUUID(config common.ClientConfig) error {
		db := dbcore.GetDBInstance()
		result := db.Model(&common.ClientConfig{}).Where("client_uuid = ?", config.ClientUUID).Updates(config)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	}
*/
func EditClientToken(clientUUID, token string) error {
	db := dbcore.GetDBInstance()
	err := db.Model(&models.Client{}).Where("uuid = ?", clientUUID).Update("token", token).Error
	if err != nil {
		return err
	}
	return nil
}

// CreateClient 创建新客户端
func CreateClient() (clientUUID, token string, err error) {
	db := dbcore.GetDBInstance()
	token = utils.GenerateToken()
	clientUUID = uuid.New().String()

	client := models.Client{
		UUID:      clientUUID,
		Token:     token,
		Name:      "client_" + clientUUID[0:8],
		CreatedAt: models.FromTime(time.Now()),
		UpdatedAt: models.FromTime(time.Now()),
	}

	err = db.Create(&client).Error
	if err != nil {
		return "", "", err
	}
	return clientUUID, token, nil
}

func CreateClientWithName(name string) (clientUUID, token string, err error) {
	if name == "" {
		return CreateClient()
	}
	db := dbcore.GetDBInstance()
	token = utils.GenerateToken()
	clientUUID = uuid.New().String()
	client := models.Client{
		UUID:      clientUUID,
		Token:     token,
		Name:      name,
		CreatedAt: models.FromTime(time.Now()),
		UpdatedAt: models.FromTime(time.Now()),
	}

	err = db.Create(&client).Error
	if err != nil {
		return "", "", err
	}
	return clientUUID, token, nil
}

/*
// GetAllClients 获取所有客户端配置

	func getAllClients() (clients []models.Client, err error) {
		db := dbcore.GetDBInstance()
		err = db.Find(&clients).Error
		if err != nil {
			return nil, err
		}
		return clients, nil
	}
*/
func GetClientByUUID(uuid string) (client models.Client, err error) {
	db := dbcore.GetDBInstance()
	err = db.Where("uuid = ?", uuid).First(&client).Error
	if err != nil {
		return models.Client{}, err
	}
	return client, nil
}

// GetClientBasicInfo 获取指定 UUID 的客户端基本信息
func GetClientBasicInfo(uuid string) (client models.Client, err error) {
	db := dbcore.GetDBInstance()
	err = db.Where("uuid = ?", uuid).First(&client).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return models.Client{}, fmt.Errorf("客户端不存在: %s", uuid)
		}
		return models.Client{}, err
	}
	return client, nil
}

func GetClientTokenByUUID(uuid string) (token string, err error) {
	db := dbcore.GetDBInstance()
	var client models.Client
	err = db.Where("uuid = ?", uuid).First(&client).Error
	if err != nil {
		return "", err
	}
	return client.Token, nil
}

func GetAllClientBasicInfo() (clients []models.Client, err error) {
	db := dbcore.GetDBInstance()
	err = db.Find(&clients).Error
	if err != nil {
		return nil, err
	}
	return clients, nil
}

func SaveClient(updates map[string]interface{}) error {
	db := dbcore.GetDBInstance()
	clientUUID, ok := updates["uuid"].(string)
	if !ok || clientUUID == "" {
		return fmt.Errorf("invalid client UUID")
	}

	// 确保更新的字段不为空
	if len(updates) == 0 {
		return fmt.Errorf("no fields to update")
	}

	updates["updated_at"] = time.Now()

	err := db.Model(&models.Client{}).Where("uuid = ?", clientUUID).Updates(updates).Error
	if err != nil {
		return err
	}
	return nil
}
