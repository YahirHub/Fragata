package camera

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net"
	"net/url"
	"strings"
	"time"
	"unicode"

	"fragata/internal/model"
	"fragata/internal/onvif"
	fragrtsp "fragata/internal/rtsp"
)

type UpdateRequest struct {
	Name                   *string              `json:"name,omitempty"`
	Host                   *string              `json:"host,omitempty"`
	Username               *string              `json:"username,omitempty"`
	Password               *string              `json:"password,omitempty"`
	RTSPURL                *string              `json:"rtsp_url,omitempty"`
	FolderName             *string              `json:"folder_name,omitempty"`
	Enabled                *bool                `json:"enabled,omitempty"`
	Record                 *bool                `json:"record,omitempty"`
	SegmentDurationSeconds *int64               `json:"segment_duration_seconds,omitempty"`
	Upload                 *bool                `json:"upload,omitempty"`
	SFTPProfileID          *string              `json:"sftp_profile_id,omitempty"`
	SnapshotURL            *string              `json:"snapshot_url,omitempty"`
	DetectionEnabled       *bool                `json:"detection_enabled,omitempty"`
	DetectMotion           *bool                `json:"detect_motion,omitempty"`
	DetectPerson           *bool                `json:"detect_person,omitempty"`
	MotionSensitivity      *int                 `json:"motion_sensitivity,omitempty"`
	DetectionIntervalSecs  *int                 `json:"detection_interval_seconds,omitempty"`
	PersonConfidence       *int                 `json:"person_confidence,omitempty"`
	DetectionCooldownSecs  *int                 `json:"detection_cooldown_seconds,omitempty"`
	DetectionZone          *model.DetectionZone `json:"detection_zone,omitempty"`
}

func (m *Manager) Update(ctx context.Context, id string, request UpdateRequest) (model.Camera, bool, error) {
	current, exists := m.store.Camera(id)
	if !exists {
		return model.Camera{}, false, errors.New("cámara no encontrada")
	}
	current = m.normalizeCamera(current)
	updated := current

	if request.Name != nil {
		name := strings.TrimSpace(*request.Name)
		if name == "" {
			return model.Camera{}, false, errors.New("el nombre de la cámara es obligatorio")
		}
		if len([]rune(name)) > 100 {
			return model.Camera{}, false, errors.New("el nombre de la cámara no puede superar 100 caracteres")
		}
		updated.Name = name
	}
	if request.FolderName != nil {
		folder, err := normalizeFolderName(*request.FolderName, updated.Name)
		if err != nil {
			return model.Camera{}, false, err
		}
		if !m.folderAvailable(folder, id) {
			return model.Camera{}, false, errors.New("otra cámara ya utiliza esa carpeta de grabación")
		}
		updated.FolderName = folder
	}
	if request.Enabled != nil {
		updated.Enabled = *request.Enabled
	}
	if request.Record != nil {
		updated.Record = *request.Record
	}
	if request.Upload != nil {
		updated.Upload = *request.Upload
	}
	if request.SFTPProfileID != nil {
		profileID := strings.TrimSpace(*request.SFTPProfileID)
		if profileID != "" && (m.uploader == nil || !m.uploader.Enabled(profileID)) {
			return model.Camera{}, false, errors.New("el servidor SFTP seleccionado no está disponible")
		}
		updated.SFTPProfileID = profileID
	}
	if updated.Upload && (m.uploader == nil || !m.uploader.Enabled(updated.SFTPProfileID)) {
		return model.Camera{}, false, errors.New("seleccione un servidor SFTP habilitado antes de activar las subidas")
	}
	if request.SegmentDurationSeconds != nil {
		if err := validateSegmentDuration(*request.SegmentDurationSeconds); err != nil {
			return model.Camera{}, false, err
		}
		updated.SegmentDurationSeconds = *request.SegmentDurationSeconds
	}
	if request.DetectionEnabled != nil {
		updated.DetectionEnabled = *request.DetectionEnabled
	}
	if request.DetectMotion != nil {
		updated.DetectMotion = *request.DetectMotion
	}
	if request.DetectPerson != nil {
		updated.DetectPerson = *request.DetectPerson
	}
	if request.MotionSensitivity != nil {
		if *request.MotionSensitivity < 1 || *request.MotionSensitivity > 100 {
			return model.Camera{}, false, errors.New("la sensibilidad debe estar entre 1 y 100")
		}
		updated.MotionSensitivity = *request.MotionSensitivity
	}
	if request.DetectionIntervalSecs != nil {
		if *request.DetectionIntervalSecs < 1 || *request.DetectionIntervalSecs > 60 {
			return model.Camera{}, false, errors.New("el intervalo de detección debe estar entre 1 y 60 segundos")
		}
		updated.DetectionIntervalSecs = *request.DetectionIntervalSecs
	}
	if request.PersonConfidence != nil {
		if *request.PersonConfidence < 40 || *request.PersonConfidence > 95 {
			return model.Camera{}, false, errors.New("la confianza humana debe estar entre 40 y 95")
		}
		updated.PersonConfidence = *request.PersonConfidence
	}
	if request.DetectionCooldownSecs != nil {
		if *request.DetectionCooldownSecs < 1 || *request.DetectionCooldownSecs > 3600 {
			return model.Camera{}, false, errors.New("el enfriamiento debe estar entre 1 y 3600 segundos")
		}
		updated.DetectionCooldownSecs = *request.DetectionCooldownSecs
	}
	if request.DetectionZone != nil {
		zone := request.DetectionZone.Normalized()
		updated.DetectionZone = zone
	}

	connectionChanged := false
	rtspURLChanged := false
	if request.Host != nil {
		host := strings.TrimSpace(*request.Host)
		if host != current.Host {
			connectionChanged = true
		}
		updated.Host = host
	}
	if request.Username != nil {
		username := strings.TrimSpace(*request.Username)
		if username != current.Username {
			connectionChanged = true
		}
		updated.Username = username
	}
	if request.RTSPURL != nil {
		rawURL := strings.TrimSpace(*request.RTSPURL)
		if rawURL == model.RedactURL(current.RTSPURL) {
			rawURL = current.RTSPURL
		}
		if rawURL != current.RTSPURL {
			connectionChanged = true
			rtspURLChanged = true
		}
		updated.RTSPURL = rawURL
	}
	if request.SnapshotURL != nil {
		rawSnapshot := strings.TrimSpace(*request.SnapshotURL)
		if rawSnapshot == model.RedactURL(current.SnapshotURL) {
			rawSnapshot = current.SnapshotURL
		}
		if rawSnapshot != "" {
			validated, err := validateSnapshotURL(rawSnapshot, updated.Host)
			if err != nil {
				return model.Camera{}, false, err
			}
			rawSnapshot = validated
		}
		updated.SnapshotURL = rawSnapshot
	}
	if request.Password != nil && *request.Password != "" {
		if *request.Password != current.Password {
			connectionChanged = true
		}
		updated.Password = *request.Password
	}

	redetected := false
	if connectionChanged {
		rawURL := ""
		if rtspURLChanged {
			rawURL = updated.RTSPURL
		}
		enabled, record, upload := updated.Enabled, updated.Record, updated.Upload
		detectRequest := AddRequest{
			Name: updated.Name, Host: updated.Host, Username: updated.Username, Password: updated.Password,
			RTSPURL: rawURL, FolderName: updated.FolderName, Enabled: &enabled, Record: &record,
			SegmentDurationSeconds: updated.SegmentDurationSeconds, Upload: &upload, SFTPProfileID: updated.SFTPProfileID, SnapshotURL: updated.SnapshotURL,
		}
		detected, err := Detect(ctx, m.cfg, detectRequest)
		if err != nil && !rtspURLChanged && current.RTSPURL != "" {
			detectRequest.RTSPURL = fragrtsp.NormalizeHost(current.RTSPURL, updated.Host)
			detected, err = Detect(ctx, m.cfg, detectRequest)
		}
		if err != nil {
			return model.Camera{}, false, err
		}
		camera := detected.Camera
		camera.ID = current.ID
		camera.CreatedAt = current.CreatedAt
		camera.Name = updated.Name
		camera.FolderName = updated.FolderName
		camera.Enabled = updated.Enabled
		camera.Record = updated.Record
		camera.SegmentDurationSeconds = updated.SegmentDurationSeconds
		camera.Upload = updated.Upload
		camera.SFTPProfileID = updated.SFTPProfileID
		if camera.SnapshotURL == "" {
			camera.SnapshotURL = updated.SnapshotURL
		}
		camera.DetectionEnabled = updated.DetectionEnabled
		camera.DetectMotion = updated.DetectMotion
		camera.DetectPerson = updated.DetectPerson
		camera.MotionSensitivity = updated.MotionSensitivity
		camera.DetectionIntervalSecs = updated.DetectionIntervalSecs
		camera.PersonConfidence = updated.PersonConfidence
		camera.DetectionCooldownSecs = updated.DetectionCooldownSecs
		camera.DetectionZone = updated.DetectionZone
		updated = camera
		redetected = true
	}

	if updated.DetectionEnabled && updated.SnapshotURL == "" {
		client := onvif.NewClient(m.cfg.ProbeTimeout, updated.Username, updated.Password, false)
		inspection, err := client.Inspect(ctx, updated.Host)
		if err == nil {
			candidate := ""
			if updated.ProfileToken != "" {
				candidate = inspection.SnapshotURIs[updated.ProfileToken]
			}
			if candidate == "" {
				for _, value := range inspection.SnapshotURIs {
					candidate = value
					break
				}
			}
			if candidate != "" {
				candidate = normalizeSnapshotHost(candidate, updated.Host)
				if validated, validateErr := validateSnapshotURL(candidate, updated.Host); validateErr == nil {
					updated.SnapshotURL = validated
				}
			}
		}
	}
	if updated.DetectionEnabled {
		if updated.SnapshotURL == "" {
			return model.Camera{}, false, errors.New("la cámara no publicó una URL de snapshot ONVIF; introdúzcala manualmente")
		}
		if !updated.DetectMotion && !updated.DetectPerson {
			return model.Camera{}, false, errors.New("active movimiento o detección de personas")
		}
		client := onvif.NewClient(10*time.Second, updated.Username, updated.Password, false)
		raw, _, err := client.FetchSnapshot(ctx, updated.SnapshotURL, 8<<20)
		if err != nil {
			return model.Camera{}, false, fmt.Errorf("no se pudo abrir el snapshot: %w", err)
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil {
			return model.Camera{}, false, fmt.Errorf("el snapshot no contiene una imagen válida: %w", err)
		}
		if config.Width < 1 || config.Height < 1 || config.Width > 10000 || config.Height > 10000 || int64(config.Width)*int64(config.Height) > 32_000_000 {
			return model.Camera{}, false, fmt.Errorf("el snapshot tiene dimensiones no permitidas: %dx%d", config.Width, config.Height)
		}
	}

	if camerasEqualForUpdate(current, updated) {
		return current, redetected, nil
	}
	updated.UpdatedAt = time.Now().UTC()
	if err := m.store.SaveCamera(updated); err != nil {
		return model.Camera{}, false, err
	}

	detectionChanged := current.SnapshotURL != updated.SnapshotURL || current.DetectionEnabled != updated.DetectionEnabled || current.DetectMotion != updated.DetectMotion ||
		current.DetectPerson != updated.DetectPerson || current.MotionSensitivity != updated.MotionSensitivity || current.DetectionIntervalSecs != updated.DetectionIntervalSecs ||
		current.PersonConfidence != updated.PersonConfidence || current.DetectionCooldownSecs != updated.DetectionCooldownSecs || current.DetectionZone != updated.DetectionZone
	restart := connectionChanged || detectionChanged || current.Enabled != updated.Enabled || current.FolderName != updated.FolderName || current.Upload != updated.Upload || current.SFTPProfileID != updated.SFTPProfileID
	if restart {
		m.restartWorker(updated)
	} else {
		m.mu.RLock()
		worker := m.workers[id]
		m.mu.RUnlock()
		if worker != nil {
			worker.configureRecording(updated.Record, time.Duration(updated.SegmentDurationSeconds)*time.Second)
		}
	}
	return updated, redetected, nil
}

func validateSegmentDuration(seconds int64) error {
	if seconds < model.MinSegmentDurationSeconds || seconds > model.MaxSegmentDurationSeconds {
		return fmt.Errorf("la duración por archivo debe estar entre %d minuto y %d horas", model.MinSegmentDurationSeconds/60, model.MaxSegmentDurationSeconds/3600)
	}
	if seconds%60 != 0 {
		return errors.New("la duración por archivo debe configurarse en minutos completos")
	}
	return nil
}

func normalizeFolderName(value, fallback string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = strings.TrimSpace(fallback)
	}
	var builder strings.Builder
	lastSeparator := false
	for _, r := range value {
		r = foldFolderRune(r)
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastSeparator = false
		case r == '-', r == '_', unicode.IsSpace(r):
			if builder.Len() > 0 && !lastSeparator {
				builder.WriteByte('-')
				lastSeparator = true
			}
		default:
			if builder.Len() > 0 && !lastSeparator {
				builder.WriteByte('-')
				lastSeparator = true
			}
		}
	}
	folder := strings.Trim(builder.String(), "-_")
	if folder == "" || folder == "." || folder == ".." {
		return "", errors.New("el nombre de la carpeta no es válido")
	}
	if len([]rune(folder)) > 80 {
		return "", errors.New("el nombre de la carpeta no puede superar 80 caracteres")
	}
	return folder, nil
}

func foldFolderRune(value rune) rune {
	value = unicode.ToLower(value)
	switch value {
	case 'á', 'à', 'ä', 'â', 'ã':
		return 'a'
	case 'é', 'è', 'ë', 'ê':
		return 'e'
	case 'í', 'ì', 'ï', 'î':
		return 'i'
	case 'ó', 'ò', 'ö', 'ô', 'õ':
		return 'o'
	case 'ú', 'ù', 'ü', 'û':
		return 'u'
	case 'ñ':
		return 'n'
	case 'ç':
		return 'c'
	default:
		return value
	}
}

func (m *Manager) folderAvailable(folder, excludeID string) bool {
	for _, camera := range m.store.Cameras() {
		if camera.ID == excludeID {
			continue
		}
		if strings.EqualFold(m.normalizeCamera(camera).FolderName, folder) {
			return false
		}
	}
	return true
}

func (m *Manager) uniqueFolderName(value, fallback, excludeID string) (string, error) {
	base, err := normalizeFolderName(value, fallback)
	if err != nil {
		return "", err
	}
	if m.folderAvailable(base, excludeID) {
		return base, nil
	}
	for suffix := 2; suffix < 1000; suffix++ {
		suffixText := fmt.Sprintf("-%d", suffix)
		baseRunes := []rune(base)
		maxBase := 80 - len([]rune(suffixText))
		if len(baseRunes) > maxBase {
			baseRunes = baseRunes[:maxBase]
		}
		candidate := string(baseRunes) + suffixText
		if m.folderAvailable(candidate, excludeID) {
			return candidate, nil
		}
	}
	return "", errors.New("no se pudo asignar una carpeta única para la cámara")
}

func camerasEqualForUpdate(left, right model.Camera) bool {
	return left.Name == right.Name && left.Host == right.Host && left.Username == right.Username && left.Password == right.Password &&
		left.RTSPURL == right.RTSPURL && left.LiveRTSPURL == right.LiveRTSPURL && left.ProfileToken == right.ProfileToken &&
		left.LiveProfileToken == right.LiveProfileToken && left.Manufacturer == right.Manufacturer && left.Model == right.Model &&
		left.SerialNumber == right.SerialNumber && left.FirmwareVersion == right.FirmwareVersion && left.Codec == right.Codec &&
		left.Width == right.Width && left.Height == right.Height && left.LiveCodec == right.LiveCodec && left.LiveWidth == right.LiveWidth &&
		left.LiveHeight == right.LiveHeight && left.FolderName == right.FolderName && left.Enabled == right.Enabled && left.Record == right.Record &&
		left.SegmentDurationSeconds == right.SegmentDurationSeconds && left.Upload == right.Upload && left.SFTPProfileID == right.SFTPProfileID &&
		left.AudioCodec == right.AudioCodec && left.AudioSampleRate == right.AudioSampleRate && left.AudioChannels == right.AudioChannels &&
		left.SnapshotURL == right.SnapshotURL && left.DetectionEnabled == right.DetectionEnabled && left.DetectMotion == right.DetectMotion &&
		left.DetectPerson == right.DetectPerson && left.MotionSensitivity == right.MotionSensitivity && left.DetectionIntervalSecs == right.DetectionIntervalSecs &&
		left.PersonConfidence == right.PersonConfidence && left.DetectionCooldownSecs == right.DetectionCooldownSecs && left.DetectionZone == right.DetectionZone
}

func normalizeSnapshotHost(raw, cameraHost string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" {
		return raw
	}
	port := parsed.Port()
	host := strings.Trim(cameraHost, "[]")
	if port != "" {
		parsed.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		parsed.Host = "[" + host + "]"
	} else {
		parsed.Host = host
	}
	return parsed.String()
}

func validateSnapshotURL(raw, cameraHost string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return "", errors.New("la URL de snapshot debe usar HTTP o HTTPS")
	}
	if !strings.EqualFold(strings.Trim(parsed.Hostname(), "[]"), strings.Trim(cameraHost, "[]")) {
		return "", errors.New("la URL de snapshot debe pertenecer al host configurado para la cámara")
	}
	parsed.User = nil
	return parsed.String(), nil
}
