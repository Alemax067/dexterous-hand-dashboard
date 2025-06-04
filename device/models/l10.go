package models

import (
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"sync"
	"time"

	"hands/communication"
	"hands/component"
	"hands/define"
	"hands/device"
)

// L10Hand L10 型号手部设备实现
type L10Hand struct {
	id              string
	model           string
	handType        define.HandType
	communicator    communication.Communicator
	components      map[device.ComponentType][]device.Component
	status          device.DeviceStatus
	mutex           sync.RWMutex
	canInterface    string                  // CAN 接口名称，如 "can0"
	animationEngine *device.AnimationEngine // 动画引擎
	presetManager   *device.PresetManager   // 预设姿势管理器
}

// 在 base 基础上进行 ±delta 的扰动，范围限制在 [0, 255]
func perturb(base byte, delta int) byte {
	offset := rand.IntN(2*delta+1) - delta
	v := min(max(int(base)+offset, 0), 255)
	return byte(v)
}

// NewL10Hand 创建 L10 手部设备实例
// 参数 config 是设备配置，包含以下字段：
//   - id: 设备 ID
//   - can_service_url: CAN 服务 URL
//   - can_interface: CAN 接口名称，如 "can0"
//   - hand_type: 手型，可选值为 "left" 或 "right"，默认值为 "right"
func NewL10Hand(config map[string]any) (device.Device, error) {
	id, ok := config["id"].(string)
	if !ok {
		return nil, fmt.Errorf("缺少设备 ID 配置")
	}

	serviceURL, ok := config["can_service_url"].(string)
	if !ok {
		return nil, fmt.Errorf("缺少 can 服务 URL 配置")
	}

	canInterface, ok := config["can_interface"].(string)
	if !ok {
		canInterface = "can0" // 默认接口
	}

	handTypeStr, ok := config["hand_type"].(string)
	handType := define.HAND_TYPE_RIGHT // 默认右手
	if ok && handTypeStr == "left" {
		handType = define.HAND_TYPE_LEFT
	}

	// 创建通信客户端
	comm := communication.NewCanBridgeClient(serviceURL)

	hand := &L10Hand{
		id:           id,
		model:        "L10",
		handType:     handType,
		communicator: comm,
		components:   make(map[device.ComponentType][]device.Component),
		canInterface: canInterface,
		status: device.DeviceStatus{
			// TODO: 这里需要修改，根据实际连接情况设置，因为当前还没有实现连接和断开路由，先设置为 true
			IsConnected: true,
			IsActive:    true,
			LastUpdate:  time.Now(),
		},
	}

	// 初始化动画引擎，将 hand 自身作为 PoseExecutor
	hand.animationEngine = device.NewAnimationEngine(hand)

	// 注册默认动画
	hand.animationEngine.Register(NewL10WaveAnimation())
	hand.animationEngine.Register(NewL10SwayAnimation())

	// 初始化预设姿势管理器
	hand.presetManager = device.NewPresetManager()

	// 注册 L10 的预设姿势
	for _, preset := range GetL10Presets() {
		hand.presetManager.RegisterPreset(preset)
	}

	// 初始化组件
	if err := hand.initializeComponents(config); err != nil {
		return nil, fmt.Errorf("初始化组件失败：%w", err)
	}

	log.Printf("✅ 设备 L10 (%s, %s) 创建成功", id, handType.String())
	return hand, nil
}

// GetHandType 获取设备手型
func (h *L10Hand) GetHandType() define.HandType {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return h.handType
}

// SetHandType 设置设备手型
func (h *L10Hand) SetHandType(handType define.HandType) error {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	if handType != define.HAND_TYPE_LEFT && handType != define.HAND_TYPE_RIGHT {
		return fmt.Errorf("无效的手型：%d", handType)
	}
	h.handType = handType
	log.Printf("🔧 设备 %s 手型已更新: %s", h.id, handType.String())
	return nil
}

// GetAnimationEngine 获取动画引擎
func (h *L10Hand) GetAnimationEngine() *device.AnimationEngine {
	return h.animationEngine
}

// SetFingerPose 设置手指姿态 (实现 PoseExecutor)
func (h *L10Hand) SetFingerPose(pose []byte) error {
	if len(pose) != 6 {
		return fmt.Errorf("无效的手指姿态数据长度，需要 6 个字节")
	}

	// 添加随机扰动
	perturbedPose := make([]byte, len(pose))
	for i, v := range pose {
		perturbedPose[i] = perturb(v, 5)
	}

	// 创建指令
	cmd := device.NewFingerPoseCommand(perturbedPose)

	// 执行指令
	err := h.ExecuteCommand(cmd)
	if err == nil {
		log.Printf("✅ %s (%s) 手指动作已发送: [%X %X %X %X %X %X]",
			h.id, h.GetHandType().String(), perturbedPose[0], perturbedPose[1], perturbedPose[2],
			perturbedPose[3], perturbedPose[4], perturbedPose[5])
	}
	return err
}

// SetPalmPose 设置手掌姿态 (实现 PoseExecutor)
func (h *L10Hand) SetPalmPose(pose []byte) error {
	if len(pose) != 4 {
		return fmt.Errorf("无效的手掌姿态数据长度，需要 4 个字节")
	}

	// 添加随机扰动
	perturbedPose := make([]byte, len(pose))
	for i, v := range pose {
		perturbedPose[i] = perturb(v, 8)
	}

	// 创建指令
	cmd := device.NewPalmPoseCommand(perturbedPose)

	// 执行指令
	err := h.ExecuteCommand(cmd)
	if err == nil {
		log.Printf("✅ %s (%s) 掌部姿态已发送: [%X %X %X %X]",
			h.id, h.GetHandType().String(), perturbedPose[0], perturbedPose[1], perturbedPose[2], perturbedPose[3])
	}
	return err
}

// ResetPose 重置到默认姿态 (实现 PoseExecutor)
func (h *L10Hand) ResetPose() error {
	log.Printf("🔄 正在重置设备 %s (%s) 到默认姿态...", h.id, h.GetHandType().String())
	defaultFingerPose := []byte{64, 64, 64, 64, 64, 64} // 0x40 - 半开
	defaultPalmPose := []byte{128, 128, 128, 128}       // 0x80 - 居中

	if err := h.SetFingerPose(defaultFingerPose); err != nil {
		log.Printf("❌ %s 重置手指姿势失败: %v", h.id, err)
		return err
	}
	time.Sleep(20 * time.Millisecond) // 短暂延时
	if err := h.SetPalmPose(defaultPalmPose); err != nil {
		log.Printf("❌ %s 重置掌部姿势失败: %v", h.id, err)
		return err
	}
	log.Printf("✅ 设备 %s 已重置到默认姿态", h.id)
	return nil
}

// commandToRawMessageUnsafe 将通用指令转换为 L10 特定的 CAN 消息（不加锁版本）
// 注意：此方法不是线程安全的，只应在已获取适当锁的情况下调用
func (h *L10Hand) commandToRawMessageUnsafe(cmd device.Command) (communication.RawMessage, error) {
	var data []byte
	canID := uint32(h.handType)

	switch cmd.Type() {
	case "SetFingerPose":
		// 添加 0x01 前缀
		data = append([]byte{0x01}, cmd.Payload()...)
		if len(data) > 8 { // CAN 消息数据长度限制
			return communication.RawMessage{}, fmt.Errorf("手指姿态数据过长")
		}
	case "SetPalmPose":
		// 添加 0x04 前缀
		data = append([]byte{0x04}, cmd.Payload()...)
		if len(data) > 8 { // CAN 消息数据长度限制
			return communication.RawMessage{}, fmt.Errorf("手掌姿态数据过长")
		}
	default:
		return communication.RawMessage{}, fmt.Errorf("L10 不支持的指令类型: %s", cmd.Type())
	}

	return communication.RawMessage{
		Interface: h.canInterface,
		ID:        canID,
		Data:      data,
	}, nil
}

// ExecuteCommand 执行一个通用指令
func (h *L10Hand) ExecuteCommand(cmd device.Command) error {
	h.mutex.Lock() // 使用写锁，因为会更新状态
	defer h.mutex.Unlock()

	if !h.status.IsConnected || !h.status.IsActive {
		return fmt.Errorf("设备 %s 未连接或未激活", h.id)
	}

	// 转换指令为 CAN 消息（使用不加锁版本，因为已经在写锁保护下）
	rawMsg, err := h.commandToRawMessageUnsafe(cmd)
	if err != nil {
		h.status.ErrorCount++
		h.status.LastError = err.Error()
		return fmt.Errorf("转换指令失败：%w", err)
	}

	// 创建带有超时的 context，设置 3 秒超时
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// 发送到 can-bridge 服务
	if err := h.communicator.SendMessage(ctx, rawMsg); err != nil {
		h.status.ErrorCount++
		h.status.LastError = err.Error()
		log.Printf("❌ %s (%s) 发送指令失败: %v (ID: 0x%X, Data: %X)", h.id, h.handType.String(), err, rawMsg.ID, rawMsg.Data)
		return fmt.Errorf("发送指令失败：%w", err)
	}

	h.status.LastUpdate = time.Now()

	return nil
}

func (h *L10Hand) initializeComponents(_ map[string]any) error {
	// 初始化传感器组件
	defaultSensor := component.NewSensorData(h.canInterface)
	defaultSensor.MockData()
	sensors := []device.Component{defaultSensor}
	h.components[device.SensorComponent] = sensors
	return nil
}

func (h *L10Hand) GetID() string {
	return h.id
}

func (h *L10Hand) GetModel() string {
	return h.model
}

func (h *L10Hand) ReadSensorData() (device.SensorData, error) {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	sensors := h.components[device.SensorComponent]
	for _, comp := range sensors {
		if sensor, ok := comp.(component.Sensor); ok {
			return sensor.ReadData()
		}
	}
	return nil, fmt.Errorf("传感器不存在")
}

func (h *L10Hand) GetComponents(componentType device.ComponentType) []device.Component {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	if components, exists := h.components[componentType]; exists {
		result := make([]device.Component, len(components))
		copy(result, components)
		return result
	}
	return []device.Component{}
}

func (h *L10Hand) GetStatus() (device.DeviceStatus, error) {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return h.status, nil
}

func (h *L10Hand) Connect() error {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	// TODO: 假设连接总是成功，除非有显式错误
	h.status.IsConnected = true
	h.status.IsActive = true
	h.status.LastUpdate = time.Now()
	log.Printf("🔗 设备 %s 已连接", h.id)
	return nil
}

func (h *L10Hand) Disconnect() error {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	h.status.IsConnected = false
	h.status.IsActive = false
	h.status.LastUpdate = time.Now()
	log.Printf("🔌 设备 %s 已断开", h.id)
	return nil
}

// --- 预设姿势相关方法 ---

// GetSupportedPresets 获取支持的预设姿势列表
func (h *L10Hand) GetSupportedPresets() []string { return h.presetManager.GetSupportedPresets() }

// ExecutePreset 执行预设姿势
func (h *L10Hand) ExecutePreset(presetName string) error {
	preset, exists := h.presetManager.GetPreset(presetName)
	if !exists {
		return fmt.Errorf("预设姿势 '%s' 不存在", presetName)
	}

	log.Printf("🎯 设备 %s (%s) 执行预设姿势: %s", h.id, h.GetHandType().String(), presetName)

	// 执行手指姿态
	if err := h.SetFingerPose(preset.FingerPose); err != nil {
		return fmt.Errorf("执行预设姿势 '%s' 的手指姿态失败: %w", presetName, err)
	}

	// 如果有手掌姿态数据，也执行
	if len(preset.PalmPose) > 0 {
		time.Sleep(20 * time.Millisecond) // 短暂延时
		if err := h.SetPalmPose(preset.PalmPose); err != nil {
			return fmt.Errorf("执行预设姿势 '%s' 的手掌姿态失败: %w", presetName, err)
		}
	}

	log.Printf("✅ 设备 %s 预设姿势 '%s' 执行完成", h.id, presetName)
	return nil
}

// GetPresetDescription 获取预设姿势描述
func (h *L10Hand) GetPresetDescription(presetName string) string {
	return h.presetManager.GetPresetDescription(presetName)
}

// GetPresetDetails 获取预设姿势详细信息
func (h *L10Hand) GetPresetDetails(presetName string) (device.PresetPose, bool) {
	return h.presetManager.GetPreset(presetName)
}

func (h *L10Hand) GetCanStatus() (map[string]bool, error) {
	return h.communicator.GetAllInterfaceStatuses()
}
