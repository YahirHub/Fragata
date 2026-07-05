package camera

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"fragata/internal/model"
	fragrtsp "fragata/internal/rtsp"
)

type UpdateRequest struct {
	Name                   *string `json:"name,omitempty"`
	Host                   *string `json:"host,omitempty"`
	Username               *string `json:"username,omitempty"`
	Password               *string `json:"password,omitempty"`
	RTSPURL                *string `json:"rtsp_url,omitempty"`
	FolderName             *string `json:"folder_name,omitempty"`
	Enabled                *bool   `json:"enabled,omitempty"`
	Record                 *bool   `json:"record,omitempty"`
	SegmentDurationSeconds *int64  `json:"segment_duration_seconds,omitempty"`
	Upload                 *bool   `json:"upload,omitempty"`
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
	if request.SegmentDurationSeconds != nil {
		if err := validateSegmentDuration(*request.SegmentDurationSeconds); err != nil {
			return model.Camera{}, false, err
		}
		updated.SegmentDurationSeconds = *request.SegmentDurationSeconds
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
			SegmentDurationSeconds: updated.SegmentDurationSeconds, Upload: &upload,
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
		updated = camera
		redetected = true
	}

	if camerasEqualForUpdate(current, updated) {
		return current, redetected, nil
	}
	updated.UpdatedAt = time.Now().UTC()
	if err := m.store.SaveCamera(updated); err != nil {
		return model.Camera{}, false, err
	}

	restart := connectionChanged || current.Enabled != updated.Enabled || current.FolderName != updated.FolderName || current.Upload != updated.Upload
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
		left.SegmentDurationSeconds == right.SegmentDurationSeconds && left.Upload == right.Upload
}
