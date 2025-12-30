package services

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"sandroidx.com/sandroidx_lite/configs"
	"sandroidx.com/sandroidx_lite/models"
	"sandroidx.com/sandroidx_lite/utils"
)

type ApkService interface {
	// CreateApk 创建 APK 记录：
	// - type=remote：必须提供 url；path 初始为空字符串，下载成功后写入
	// - type=local：不允许手工传 path/url，应走 UploadLocalApk
	CreateApk(name, packageName, version, urlStr, typ, description string) (*models.Apk, error)
	GetApk(id string) (*models.Apk, error)
	ListApks() ([]models.Apk, error)
	UpdateApk(id string, name, packageName, version, urlStr, typ, description *string) (*models.Apk, error)
	DeleteApk(id string) error

	// UploadLocalApk 上传本地 APK 并创建记录（type=local），落盘到 data_path/apks 后写入 path
	UploadLocalApk(file *multipart.FileHeader, name, packageName, version, description string) (*models.Apk, error)
	// UploadLocalApkFromTemp 从临时文件创建 APK 记录（用于预览后的保存，避免重复上传）
	UploadLocalApkFromTemp(tempPath, name, packageName, version, description string) (*models.Apk, error)
	// UploadLocalApkPreview 上传本地 APK 并解析信息（不创建记录），返回解析结果
	UploadLocalApkPreview(file *multipart.FileHeader) (*ApkPreviewResult, error)
	// DownloadRemoteApk 下载 remote APK 到 data_path/apks，并将本地路径写入 path
	DownloadRemoteApk(id string) (*models.Apk, error)
	// DownloadRemoteApkPreview 根据 URL 下载 APK 并解析信息（不创建记录），返回解析结果
	DownloadRemoteApkPreview(urlStr string) (*ApkPreviewResult, error)
	// FindOrPrepareApk 查找或准备 APK：根据 package_name 和 version 查找，如果不存在且是 remote 则下载并校验后存入库
	// 返回本地 APK 文件路径（可用于安装）
	FindOrPrepareApk(packageName, version, urlStr, name string) (string, error)
}

// ApkPreviewResult APK 预览解析结果（用于上传/下载预览，不创建记录）
type ApkPreviewResult struct {
	PackageName string `json:"package_name"`
	Version     string `json:"version"`
	TempPath    string `json:"temp_path"` // 临时文件路径（用于后续创建 APK，避免重复上传）
}

// downloadingApk 表示正在下载的APK信息
type downloadingApk struct {
	done chan struct{} // 下载完成信号
	err  error         // 下载错误
	path string        // 下载成功后的路径
}

type apkService struct {
	// 并发控制：跟踪正在下载的APK，key为 packageName:version
	downloading map[string]*downloadingApk
	mu          sync.Mutex
}

func NewApkService() ApkService {
	return &apkService{
		downloading: make(map[string]*downloadingApk),
	}
}

func (s *apkService) CreateApk(name, packageName, version, urlStr, typ, description string) (*models.Apk, error) {
	name = strings.TrimSpace(name)
	packageName = strings.TrimSpace(packageName)
	version = strings.TrimSpace(version)
	urlStr = strings.TrimSpace(urlStr)
	typ = strings.TrimSpace(typ)

	if name == "" {
		return nil, errors.New("名称不能为空")
	}
	// packageName 和 version 现在是可选的（会优先自动解析）
	if typ != models.ApkTypeLocal && typ != models.ApkTypeRemote {
		return nil, fmt.Errorf("type 不合法，仅支持 %s/%s", models.ApkTypeLocal, models.ApkTypeRemote)
	}
	if typ == models.ApkTypeLocal {
		return nil, errors.New("local 类型必须通过上传创建")
	}

	// remote：必须提供 url
	if urlStr == "" {
		return nil, errors.New("remote 类型必须提供 url")
	}
	u, err := url.Parse(urlStr)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, errors.New("url 必须是有效 URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("url 仅支持 http/https")
	}

	// 校验同包名 + 版本唯一（这里先跳过，因为后面会自动解析后重新校验）

	// remote 类型：必须先下载并校验为有效 APK，校验通过后才能创建记录
	dir, err := s.ensureApkDir()
	if err != nil {
		return nil, err
	}

	userPackageName := packageName
	userVersion := version

	// 先使用临时文件名下载
	tempDstName := fmt.Sprintf("temp_%d.apk", time.Now().Unix())
	dstPath := filepath.Join(dir, tempDstName)

	// 下载文件（使用配置中的最大文件大小和超时时间）
	maxBytes := int64(1073741824) // 默认 1GB
	timeoutSeconds := int64(1800) // 默认 30 分钟
	if configs.AppConfig.Upload.MaxSizeBytes > 0 {
		maxBytes = configs.AppConfig.Upload.MaxSizeBytes
	}
	if configs.AppConfig.Upload.TimeoutSeconds > 0 {
		timeoutSeconds = configs.AppConfig.Upload.TimeoutSeconds
	}
	client := &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second}
	resp, err := client.Get(urlStr)
	if err != nil {
		return nil, fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(dstPath)
	if err != nil {
		return nil, fmt.Errorf("创建目标文件失败: %w", err)
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, io.LimitReader(resp.Body, maxBytes)); err != nil {
		_ = os.Remove(dstPath) // 清理失败的文件
		return nil, fmt.Errorf("保存下载文件失败: %w", err)
	}

	// 校验文件是否为有效的 APK
	if err := utils.ValidateApkFile(dstPath); err != nil {
		_ = os.Remove(dstPath) // 清理无效的文件
		return nil, fmt.Errorf("下载的文件不是有效的 APK: %w", err)
	}

	// 尝试自动解析 APK
	var finalPackageName, finalVersion, iconPath string
	apkInfo, err := utils.ParseApkFile(dstPath, dir)
	if err == nil && apkInfo != nil {
		// 解析成功，优先使用解析的值
		if apkInfo.PackageName != "" {
			finalPackageName = apkInfo.PackageName
		}
		if apkInfo.VersionName != "" {
			finalVersion = apkInfo.VersionName
		} else if apkInfo.VersionCode > 0 {
			finalVersion = fmt.Sprintf("%d", apkInfo.VersionCode)
		}
		iconPath = apkInfo.IconPath
	}

	// 如果解析失败或解析值为空，使用用户提供的值
	if finalPackageName == "" {
		finalPackageName = userPackageName
	}
	if finalVersion == "" {
		finalVersion = userVersion
	}

	// 最终校验：必须要有包名和版本
	if finalPackageName == "" {
		_ = os.Remove(dstPath)
		return nil, errors.New("无法解析包名，请手动填写")
	}
	if finalVersion == "" {
		_ = os.Remove(dstPath)
		return nil, errors.New("无法解析版本，请手动填写")
	}

	// 重新命名文件（使用最终确定的包名和版本）
	finalDstName := utils.BuildApkFilename(finalPackageName, finalVersion, filepath.Base(u.Path))
	finalDstPath := filepath.Join(dir, finalDstName)
	if dstPath != finalDstPath {
		if err := os.Rename(dstPath, finalDstPath); err != nil {
			_ = os.Remove(dstPath)
			return nil, fmt.Errorf("重命名文件失败: %w", err)
		}
		dstPath = finalDstPath
	}

	// 唯一性校验（避免覆盖）
	var cnt int64
	if err := models.DB.Model(&models.Apk{}).Where("package_name = ? AND version = ?", finalPackageName, finalVersion).Count(&cnt).Error; err != nil {
		_ = os.Remove(dstPath)
		return nil, fmt.Errorf("检查包名版本失败: %w", err)
	}
	if cnt > 0 {
		_ = os.Remove(dstPath)
		return nil, errors.New("包名+版本已存在")
	}

	// 校验通过，创建记录
	now := time.Now()
	apk := &models.Apk{
		ID:          utils.GenerateApkID(),
		Name:        name,
		PackageName: finalPackageName,
		Version:     finalVersion,
		Path:        dstPath, // 下载并校验成功后写入
		URL:         urlStr,
		Icon:        iconPath,
		Type:        typ,
		Description: description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := models.DB.Create(apk).Error; err != nil {
		_ = os.Remove(dstPath) // 创建记录失败，清理文件
		if iconPath != "" {
			_ = os.Remove(iconPath)
		}
		return nil, fmt.Errorf("创建 APK 记录失败: %w", err)
	}
	return apk, nil
}

func (s *apkService) GetApk(id string) (*models.Apk, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("ID 不能为空")
	}

	var apk models.Apk
	if err := models.DB.First(&apk, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("APK 不存在")
		}
		return nil, fmt.Errorf("查询 APK 失败: %w", err)
	}
	return &apk, nil
}

func (s *apkService) ListApks() ([]models.Apk, error) {
	var list []models.Apk
	if err := models.DB.Order("created_at desc").Find(&list).Error; err != nil {
		return nil, fmt.Errorf("查询 APK 列表失败: %w", err)
	}
	return list, nil
}

func (s *apkService) UpdateApk(id string, name, packageName, version, urlStr, typ, description *string) (*models.Apk, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("ID 不能为空")
	}

	apk, err := s.GetApk(id)
	if err != nil {
		return nil, err
	}

	updates := map[string]interface{}{
		"updated_at": time.Now(),
	}
	changed := false

	if name != nil {
		n := strings.TrimSpace(*name)
		if n == "" {
			return nil, errors.New("名称不能为空")
		}
		updates["name"] = n
		changed = true
	}

	if packageName != nil {
		pn := strings.TrimSpace(*packageName)
		if pn == "" {
			return nil, errors.New("包名不能为空")
		}
		updates["package_name"] = pn
		changed = true
	}

	if version != nil {
		ver := strings.TrimSpace(*version)
		if ver == "" {
			return nil, errors.New("版本不能为空")
		}
		updates["version"] = ver
		changed = true
	}

	if typ != nil {
		t := strings.TrimSpace(*typ)
		if t != models.ApkTypeLocal && t != models.ApkTypeRemote {
			return nil, fmt.Errorf("type 不合法，仅支持 %s/%s", models.ApkTypeLocal, models.ApkTypeRemote)
		}
		updates["type"] = t
		changed = true
	}

	if urlStr != nil {
		uRaw := strings.TrimSpace(*urlStr)
		updates["url"] = uRaw
		changed = true
	}

	if description != nil {
		updates["description"] = *description
		changed = true
	}

	// 若包名/版本任意更新，需要在最终值上做一次唯一性校验
	_, pkgUpdated := updates["package_name"]
	_, verUpdated := updates["version"]
	if pkgUpdated || verUpdated {
		finalPkg := apk.PackageName
		finalVer := apk.Version
		if v, ok := updates["package_name"].(string); ok && v != "" {
			finalPkg = v
		}
		if v, ok := updates["version"].(string); ok && v != "" {
			finalVer = v
		}
		var cnt int64
		if err := models.DB.Model(&models.Apk{}).
			Where("package_name = ? AND version = ? AND id <> ?", finalPkg, finalVer, id).
			Count(&cnt).Error; err != nil {
			return nil, fmt.Errorf("检查包名版本失败: %w", err)
		}
		if cnt > 0 {
			return nil, errors.New("包名+版本已存在")
		}
	}

	// remote 类型必须有 url；local 类型不允许手工改成 remote/local 造成矛盾
	finalType := apk.Type
	if v, ok := updates["type"].(string); ok && v != "" {
		finalType = v
	}
	finalURL := apk.URL
	if v, ok := updates["url"].(string); ok {
		finalURL = v
	}
	if finalType == models.ApkTypeRemote {
		if strings.TrimSpace(finalURL) == "" {
			return nil, errors.New("remote 类型必须提供 url")
		}
		u, err := url.Parse(strings.TrimSpace(finalURL))
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, errors.New("url 必须是有效 URL")
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, errors.New("url 仅支持 http/https")
		}
	}
	if finalType == models.ApkTypeLocal {
		// local 必须通过上传来保证 path 正确
		if strings.TrimSpace(apk.Path) == "" {
			// 允许“先创建再上传”的场景不存在，因此直接提示
			return nil, errors.New("local 类型必须通过上传创建")
		}
		// local 不允许手工写 url
		if urlStr != nil && strings.TrimSpace(*urlStr) != "" {
			return nil, errors.New("local 类型不允许设置 url")
		}
	}

	if !changed {
		return apk, nil
	}

	if err := models.DB.Model(&models.Apk{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("更新 APK 失败: %w", err)
	}
	return s.GetApk(id)
}

func (s *apkService) DeleteApk(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("ID 不能为空")
	}

	res := models.DB.Delete(&models.Apk{}, "id = ?", id)
	if res.Error != nil {
		return fmt.Errorf("删除 APK 失败: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return errors.New("APK 不存在")
	}
	return nil
}

func (s *apkService) ensureApkDir() (string, error) {
	if configs.AppConfig.Server.DataPath == "" {
		return "", errors.New("data_path 未配置")
	}
	dir := filepath.Join(configs.AppConfig.Server.DataPath, "apks")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("创建 apks 目录失败: %w", err)
	}
	return dir, nil
}

func (s *apkService) UploadLocalApk(file *multipart.FileHeader, name, packageName, version, description string) (*models.Apk, error) {
	if file == nil {
		return nil, errors.New("未选择文件")
	}

	name = strings.TrimSpace(name)
	userPackageName := strings.TrimSpace(packageName)
	userVersion := strings.TrimSpace(version)
	if name == "" {
		return nil, errors.New("名称不能为空")
	}

	dir, err := s.ensureApkDir()
	if err != nil {
		return nil, err
	}

	// 先保存文件
	dstName := utils.BuildApkFilename(userPackageName, userVersion, file.Filename)
	if dstName == "" || userPackageName == "" || userVersion == "" {
		// 如果用户没有提供包名或版本，使用临时文件名
		dstName = fmt.Sprintf("temp_%d.apk", time.Now().Unix())
	}
	dstPath := filepath.Join(dir, dstName)

	src, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("打开上传文件失败: %w", err)
	}
	defer src.Close()

	out, err := os.Create(dstPath)
	if err != nil {
		return nil, fmt.Errorf("创建目标文件失败: %w", err)
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, src); err != nil {
		return nil, fmt.Errorf("保存文件失败: %w", err)
	}

	// 校验文件是否为有效的 APK
	if err := utils.ValidateApkFile(dstPath); err != nil {
		_ = os.Remove(dstPath) // 清理无效的文件
		return nil, fmt.Errorf("上传的文件不是有效的 APK: %w", err)
	}

	// 尝试自动解析 APK
	var finalPackageName, finalVersion, iconPath string
	apkInfo, err := utils.ParseApkFile(dstPath, dir)
	if err == nil && apkInfo != nil {
		// 解析成功，优先使用解析的值
		if apkInfo.PackageName != "" {
			finalPackageName = apkInfo.PackageName
		}
		if apkInfo.VersionName != "" {
			finalVersion = apkInfo.VersionName
		} else if apkInfo.VersionCode > 0 {
			finalVersion = fmt.Sprintf("%d", apkInfo.VersionCode)
		}
		iconPath = apkInfo.IconPath
	}

	// 如果解析失败或解析值为空，使用用户提供的值
	if finalPackageName == "" {
		finalPackageName = userPackageName
	}
	if finalVersion == "" {
		finalVersion = userVersion
	}

	// 最终校验：必须要有包名和版本
	if finalPackageName == "" {
		_ = os.Remove(dstPath)
		return nil, errors.New("无法解析包名，请手动填写")
	}
	if finalVersion == "" {
		_ = os.Remove(dstPath)
		return nil, errors.New("无法解析版本，请手动填写")
	}

	// 重新命名文件（使用最终确定的包名和版本）
	if userPackageName != finalPackageName || userVersion != finalVersion {
		finalDstName := utils.BuildApkFilename(finalPackageName, finalVersion, file.Filename)
		finalDstPath := filepath.Join(dir, finalDstName)
		if err := os.Rename(dstPath, finalDstPath); err != nil {
			_ = os.Remove(dstPath)
			return nil, fmt.Errorf("重命名文件失败: %w", err)
		}
		dstPath = finalDstPath
	}

	// 唯一性校验（避免覆盖）
	var cnt int64
	if err := models.DB.Model(&models.Apk{}).Where("package_name = ? AND version = ?", finalPackageName, finalVersion).Count(&cnt).Error; err != nil {
		_ = os.Remove(dstPath)
		return nil, fmt.Errorf("检查包名版本失败: %w", err)
	}
	if cnt > 0 {
		_ = os.Remove(dstPath)
		return nil, errors.New("包名+版本已存在")
	}

	now := time.Now()
	apk := &models.Apk{
		ID:          utils.GenerateApkID(),
		Name:        name,
		PackageName: finalPackageName,
		Version:     finalVersion,
		Path:        dstPath,
		URL:         "",
		Icon:        iconPath,
		Type:        models.ApkTypeLocal,
		Description: description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := models.DB.Create(apk).Error; err != nil {
		_ = os.Remove(dstPath)
		if iconPath != "" {
			_ = os.Remove(iconPath)
		}
		return nil, fmt.Errorf("创建 APK 记录失败: %w", err)
	}
	return apk, nil
}

// UploadLocalApkFromTemp 从临时文件创建 APK 记录（用于预览后的保存，避免重复上传）
func (s *apkService) UploadLocalApkFromTemp(tempPath, name, packageName, version, description string) (*models.Apk, error) {
	name = strings.TrimSpace(name)
	userPackageName := strings.TrimSpace(packageName)
	userVersion := strings.TrimSpace(version)
	if name == "" {
		return nil, errors.New("名称不能为空")
	}

	// 检查临时文件是否存在
	if _, err := os.Stat(tempPath); os.IsNotExist(err) {
		return nil, errors.New("临时文件不存在")
	}

	dir, err := s.ensureApkDir()
	if err != nil {
		return nil, err
	}

	// 尝试自动解析 APK（如果还未解析）
	var finalPackageName, finalVersion, iconPath string
	apkInfo, err := utils.ParseApkFile(tempPath, dir)
	if err == nil && apkInfo != nil {
		// 解析成功，优先使用解析的值
		if apkInfo.PackageName != "" {
			finalPackageName = apkInfo.PackageName
		}
		if apkInfo.VersionName != "" {
			finalVersion = apkInfo.VersionName
		} else if apkInfo.VersionCode > 0 {
			finalVersion = fmt.Sprintf("%d", apkInfo.VersionCode)
		}
		iconPath = apkInfo.IconPath
	}

	// 如果解析失败或解析值为空，使用用户提供的值
	if finalPackageName == "" {
		finalPackageName = userPackageName
	}
	if finalVersion == "" {
		finalVersion = userVersion
	}

	// 最终校验：必须要有包名和版本
	if finalPackageName == "" {
		return nil, errors.New("无法解析包名，请手动填写")
	}
	if finalVersion == "" {
		return nil, errors.New("无法解析版本，请手动填写")
	}

	// 构建最终文件名和路径
	finalDstName := utils.BuildApkFilename(finalPackageName, finalVersion, filepath.Base(tempPath))
	finalDstPath := filepath.Join(dir, finalDstName)

	// 如果临时文件和最终路径不同，移动文件；否则直接使用
	if tempPath != finalDstPath {
		if err := os.Rename(tempPath, finalDstPath); err != nil {
			return nil, fmt.Errorf("移动文件失败: %w", err)
		}
	}

	// 唯一性校验（避免覆盖）
	var existingApk models.Apk
	if err := models.DB.Where("package_name = ? AND version = ?", finalPackageName, finalVersion).First(&existingApk).Error; err == nil {
		// 如果文件已经移动到最终位置，需要清理
		if tempPath != finalDstPath {
			_ = os.Remove(finalDstPath)
		}
		return nil, errors.New("包名+版本已存在")
	} else if err != gorm.ErrRecordNotFound {
		if tempPath != finalDstPath {
			_ = os.Remove(finalDstPath)
		}
		return nil, fmt.Errorf("检查包名版本失败: %w", err)
	}

	now := time.Now()
	apk := &models.Apk{
		ID:          utils.GenerateApkID(),
		Name:        name,
		PackageName: finalPackageName,
		Version:     finalVersion,
		Path:        finalDstPath,
		URL:         "",
		Icon:        iconPath,
		Type:        models.ApkTypeLocal,
		Description: description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := models.DB.Create(apk).Error; err != nil {
		_ = os.Remove(finalDstPath)
		if iconPath != "" {
			_ = os.Remove(iconPath)
		}
		return nil, fmt.Errorf("创建 APK 记录失败: %w", err)
	}
	return apk, nil
}

// UploadLocalApkPreview 上传本地 APK 并解析信息（不创建记录），返回解析结果
func (s *apkService) UploadLocalApkPreview(file *multipart.FileHeader) (*ApkPreviewResult, error) {
	if file == nil {
		return nil, errors.New("未选择文件")
	}

	dir, err := s.ensureApkDir()
	if err != nil {
		return nil, err
	}

	// 保存到临时文件
	tempDstName := fmt.Sprintf("temp_preview_%d.apk", time.Now().Unix())
	tempDstPath := filepath.Join(dir, tempDstName)

	src, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("打开上传文件失败: %w", err)
	}
	defer src.Close()

	out, err := os.Create(tempDstPath)
	if err != nil {
		return nil, fmt.Errorf("创建临时文件失败: %w", err)
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, src); err != nil {
		_ = os.Remove(tempDstPath)
		return nil, fmt.Errorf("保存文件失败: %w", err)
	}

	// 校验文件是否为有效的 APK
	if err := utils.ValidateApkFile(tempDstPath); err != nil {
		_ = os.Remove(tempDstPath)
		return nil, fmt.Errorf("上传的文件不是有效的 APK: %w", err)
	}

	// 尝试自动解析 APK
	result := &ApkPreviewResult{}
	apkInfo, err := utils.ParseApkFile(tempDstPath, dir)
	if err == nil && apkInfo != nil {
		// 解析成功
		if apkInfo.PackageName != "" {
			result.PackageName = apkInfo.PackageName
		}
		if apkInfo.VersionName != "" {
			result.Version = apkInfo.VersionName
		} else if apkInfo.VersionCode > 0 {
			result.Version = fmt.Sprintf("%d", apkInfo.VersionCode)
		}
		// 清理临时图标文件（预览不需要保存图标）
		if apkInfo.IconPath != "" {
			_ = os.Remove(apkInfo.IconPath)
		}
	}

	// 不删除临时文件，返回临时文件路径供后续使用
	result.TempPath = tempDstPath

	// 如果没有解析到包名或版本，返回错误（但允许空值，由前端提示用户手动填写）
	return result, nil
}

// DownloadRemoteApkPreview 根据 URL 下载 APK 并解析信息（不创建记录），返回解析结果
func (s *apkService) DownloadRemoteApkPreview(urlStr string) (*ApkPreviewResult, error) {
	urlStr = strings.TrimSpace(urlStr)
	if urlStr == "" {
		return nil, errors.New("URL 不能为空")
	}

	// 验证 URL
	u, err := url.Parse(urlStr)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, errors.New("url 必须是有效 URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("url 仅支持 http/https")
	}

	dir, err := s.ensureApkDir()
	if err != nil {
		return nil, err
	}

	// 使用临时文件名下载
	tempDstName := fmt.Sprintf("temp_preview_%d.apk", time.Now().Unix())
	tempDstPath := filepath.Join(dir, tempDstName)

	// 下载文件（使用配置中的最大文件大小和超时时间）
	maxBytes := int64(1073741824) // 默认 1GB
	timeoutSeconds := int64(1800) // 默认 30 分钟
	if configs.AppConfig.Upload.MaxSizeBytes > 0 {
		maxBytes = configs.AppConfig.Upload.MaxSizeBytes
	}
	if configs.AppConfig.Upload.TimeoutSeconds > 0 {
		timeoutSeconds = configs.AppConfig.Upload.TimeoutSeconds
	}
	client := &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second}
	resp, err := client.Get(urlStr)
	if err != nil {
		return nil, fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(tempDstPath)
	if err != nil {
		return nil, fmt.Errorf("创建临时文件失败: %w", err)
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, io.LimitReader(resp.Body, maxBytes)); err != nil {
		_ = os.Remove(tempDstPath)
		return nil, fmt.Errorf("保存下载文件失败: %w", err)
	}

	// 校验文件是否为有效的 APK
	if err := utils.ValidateApkFile(tempDstPath); err != nil {
		_ = os.Remove(tempDstPath)
		return nil, fmt.Errorf("下载的文件不是有效的 APK: %w", err)
	}

	// 尝试自动解析 APK
	result := &ApkPreviewResult{}
	apkInfo, err := utils.ParseApkFile(tempDstPath, dir)
	if err == nil && apkInfo != nil {
		// 解析成功
		if apkInfo.PackageName != "" {
			result.PackageName = apkInfo.PackageName
		}
		if apkInfo.VersionName != "" {
			result.Version = apkInfo.VersionName
		} else if apkInfo.VersionCode > 0 {
			result.Version = fmt.Sprintf("%d", apkInfo.VersionCode)
		}
		// 清理临时图标文件（预览不需要保存图标）
		if apkInfo.IconPath != "" {
			_ = os.Remove(apkInfo.IconPath)
		}
	}

	// 清理临时文件
	_ = os.Remove(tempDstPath)

	return result, nil
}

func (s *apkService) DownloadRemoteApk(id string) (*models.Apk, error) {
	apk, err := s.GetApk(id)
	if err != nil {
		return nil, err
	}
	if apk.Type != models.ApkTypeRemote {
		return nil, errors.New("仅 remote 类型支持下载")
	}
	urlStr := strings.TrimSpace(apk.URL)
	if urlStr == "" {
		return nil, errors.New("remote 类型必须提供 url")
	}
	u, err := url.Parse(urlStr)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, errors.New("url 必须是有效 URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("url 仅支持 http/https")
	}

	// 如果已经有 path，说明已经下载过了
	if strings.TrimSpace(apk.Path) != "" {
		return apk, nil
	}

	dir, err := s.ensureApkDir()
	if err != nil {
		return nil, err
	}

	dstName := utils.BuildApkFilename(apk.PackageName, apk.Version, filepath.Base(u.Path))
	dstPath := filepath.Join(dir, dstName)

	// 下载文件（使用配置中的最大文件大小和超时时间）
	maxBytes := int64(1073741824) // 默认 1GB
	timeoutSeconds := int64(1800) // 默认 30 分钟
	if configs.AppConfig.Upload.MaxSizeBytes > 0 {
		maxBytes = configs.AppConfig.Upload.MaxSizeBytes
	}
	if configs.AppConfig.Upload.TimeoutSeconds > 0 {
		timeoutSeconds = configs.AppConfig.Upload.TimeoutSeconds
	}
	client := &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second}
	resp, err := client.Get(urlStr)
	if err != nil {
		return nil, fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(dstPath)
	if err != nil {
		return nil, fmt.Errorf("创建目标文件失败: %w", err)
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, io.LimitReader(resp.Body, maxBytes)); err != nil {
		_ = os.Remove(dstPath) // 清理失败的文件
		return nil, fmt.Errorf("保存下载文件失败: %w", err)
	}

	// 校验文件是否为有效的 APK
	if err := utils.ValidateApkFile(dstPath); err != nil {
		_ = os.Remove(dstPath) // 清理无效的文件
		return nil, fmt.Errorf("下载的文件不是有效的 APK: %w", err)
	}

	// 校验通过，更新 path
	if err := models.DB.Model(&models.Apk{}).Where("id = ?", id).Updates(map[string]interface{}{
		"path":       dstPath,
		"updated_at": time.Now(),
	}).Error; err != nil {
		_ = os.Remove(dstPath) // 更新失败，清理文件
		return nil, fmt.Errorf("写入 path 失败: %w", err)
	}

	return s.GetApk(id)
}

// FindOrPrepareApk 查找或准备 APK：根据 package_name 和 version 查找，如果不存在且是 remote 则下载并校验后存入库
// 返回本地 APK 文件路径（可用于安装）
// 此方法支持并发安全：多个 goroutine 同时请求同一个 APK 时，只会下载一次
func (s *apkService) FindOrPrepareApk(packageName, version, urlStr, name string) (string, error) {
	packageName = strings.TrimSpace(packageName)
	version = strings.TrimSpace(version)
	urlStr = strings.TrimSpace(urlStr)

	if packageName == "" {
		return "", errors.New("包名不能为空")
	}
	if version == "" {
		return "", errors.New("版本不能为空")
	}

	// 生成唯一key用于并发控制
	key := fmt.Sprintf("%s:%s", packageName, version)

	// 并发控制：先检查是否正在下载
	s.mu.Lock()
	downloading, exists := s.downloading[key]
	if exists {
		// 正在下载，等待下载完成
		s.mu.Unlock()
		<-downloading.done
		if downloading.err != nil {
			return "", downloading.err
		}
		return downloading.path, nil
	}
	s.mu.Unlock()

	// 先在本地查找（在锁外检查，避免阻塞）
	var apk models.Apk
	err := models.DB.Where("package_name = ? AND version = ?", packageName, version).First(&apk).Error
	if err == nil {
		// 找到了，检查是否有本地路径
		if strings.TrimSpace(apk.Path) != "" {
			// 检查文件是否存在
			if _, err := os.Stat(apk.Path); err == nil {
				return apk.Path, nil
			}
			// 文件不存在，但记录存在，继续尝试下载（如果是 remote）
		} else if apk.Type == models.ApkTypeRemote && strings.TrimSpace(apk.URL) != "" {
			// 记录存在但 path 为空，尝试下载
			downloaded, err := s.DownloadRemoteApk(apk.ID)
			if err == nil && strings.TrimSpace(downloaded.Path) != "" {
				return downloaded.Path, nil
			}
		}
	}

	// 本地没有找到，如果是 remote 类型，则下载并创建记录
	if urlStr != "" {
		// 验证 URL
		u, err := url.Parse(urlStr)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return "", errors.New("url 必须是有效 URL")
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return "", errors.New("url 仅支持 http/https")
		}

		// 再次检查是否正在下载（双重检查，避免在检查数据库期间其他实例开始下载）
		s.mu.Lock()
		downloading, exists = s.downloading[key]
		if exists {
			// 在检查数据库期间，其他实例已经开始下载
			s.mu.Unlock()
			<-downloading.done
			if downloading.err != nil {
				return "", downloading.err
			}
			return downloading.path, nil
		}

		// 没有正在下载，开始下载
		downloading = &downloadingApk{
			done: make(chan struct{}),
		}
		s.downloading[key] = downloading
		s.mu.Unlock()

		// 下载完成后，清理并通知等待的 goroutine
		defer func() {
			s.mu.Lock()
			delete(s.downloading, key)
			s.mu.Unlock()
			close(downloading.done)
		}()

		// 创建 APK 记录（会自动下载并校验）
		apkName := name
		if apkName == "" {
			apkName = packageName
		}
		created, err := s.CreateApk(apkName, packageName, version, urlStr, models.ApkTypeRemote, "")
		if err != nil {
			downloading.err = fmt.Errorf("下载并创建 APK 记录失败: %w", err)
			return "", downloading.err
		}

		if strings.TrimSpace(created.Path) != "" {
			downloading.path = created.Path
			return created.Path, nil
		}
		downloading.err = errors.New("下载后无法获取本地路径")
		return "", downloading.err
	}

	return "", fmt.Errorf("未找到包名为 %s、版本为 %s 的 APK，且未提供下载 URL", packageName, version)
}
