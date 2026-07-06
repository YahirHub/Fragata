package httpapi

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	maxRecordingsPerDay = 5000
	maxRecordingDays    = 366
)

type recordingSource struct {
	ID         string `json:"id"`
	CameraID   string `json:"camera_id,omitempty"`
	Name       string `json:"name"`
	FolderName string `json:"folder_name"`
	Archived   bool   `json:"archived"`
}

type recordingItem struct {
	ID                string    `json:"id"`
	CameraID          string    `json:"camera_id,omitempty"`
	CameraName        string    `json:"camera_name"`
	FolderName        string    `json:"folder_name"`
	StartedAt         time.Time `json:"started_at"`
	EndedAt           time.Time `json:"ended_at"`
	DurationSeconds   float64   `json:"duration_seconds"`
	Size              int64     `json:"size"`
	Pending           bool      `json:"pending"`
	Recovered         bool      `json:"recovered"`
	PlaybackSupported bool      `json:"playback_supported"`
	PlaybackURL       string    `json:"playback_url,omitempty"`
	DownloadURL       string    `json:"download_url,omitempty"`
	EventCount        int       `json:"event_count"`

	absolute string
	relative string
}

type recordingDay struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
	Size  int64  `json:"size"`
}

type recordingTimelineEvent struct {
	ID          string    `json:"id"`
	RecordingID string    `json:"recording_id,omitempty"`
	Type        string    `json:"type"`
	CreatedAt   time.Time `json:"created_at"`
	OffsetSecs  float64   `json:"offset_seconds,omitempty"`
	DetailURL   string    `json:"detail_url"`
}

type recordingsResponse struct {
	Date             string                   `json:"date"`
	Timezone         string                   `json:"timezone"`
	Sources          []recordingSource        `json:"sources"`
	Recordings       []recordingItem          `json:"recordings"`
	Events           []recordingTimelineEvent `json:"events"`
	TotalSize        int64                    `json:"total_size"`
	TotalDurationSec float64                  `json:"total_duration_seconds"`
}

func (s *Server) recordingsPage(w http.ResponseWriter, _ *http.Request) {
	s.serveAsset(w, "recordings.html", "text/html; charset=utf-8")
}

func (s *Server) listRecordingSources(w http.ResponseWriter, _ *http.Request) {
	sources, err := s.recordingSources()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "no se pudieron consultar las fuentes de grabación")
		return
	}
	writeJSON(w, http.StatusOK, sources)
}

func (s *Server) listRecordings(w http.ResponseWriter, r *http.Request) {
	day, err := parseRecordingDay(r.URL.Query().Get("date"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sources, err := s.recordingSources()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "no se pudieron consultar las grabaciones")
		return
	}
	selected, err := selectRecordingSources(sources, strings.TrimSpace(r.URL.Query().Get("camera_id")))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	items := make([]recordingItem, 0)
	for _, source := range selected {
		listed, listErr := s.recordingsForSourceDay(source, day)
		if listErr != nil && !errors.Is(listErr, os.ErrNotExist) {
			s.logger.Warn("recording directory scan failed", "source", source.ID, "date", day.Format("2006-01-02"), "error", listErr)
			continue
		}
		items = append(items, listed...)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].StartedAt.Equal(items[j].StartedAt) {
			return items[i].CameraName < items[j].CameraName
		}
		return items[i].StartedAt.After(items[j].StartedAt)
	})
	if len(items) > maxRecordingsPerDay {
		items = items[:maxRecordingsPerDay]
	}

	events := s.timelineEvents(day, items)
	var totalSize int64
	var totalDuration float64
	for i := range items {
		totalSize += items[i].Size
		totalDuration += items[i].DurationSeconds
		items[i].absolute = ""
		items[i].relative = ""
	}
	writeJSON(w, http.StatusOK, recordingsResponse{
		Date:             day.Format("2006-01-02"),
		Timezone:         time.Local.String(),
		Sources:          sources,
		Recordings:       items,
		Events:           events,
		TotalSize:        totalSize,
		TotalDurationSec: totalDuration,
	})
}

func (s *Server) listRecordingDays(w http.ResponseWriter, r *http.Request) {
	sources, err := s.recordingSources()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "no se pudieron consultar los días grabados")
		return
	}
	selected, err := selectRecordingSources(sources, strings.TrimSpace(r.URL.Query().Get("camera_id")))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	limit := maxRecordingDays
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, convErr := strconv.Atoi(raw); convErr != nil || parsed < 1 || parsed > maxRecordingDays {
			writeError(w, http.StatusBadRequest, "limit debe estar entre 1 y 366")
			return
		} else {
			limit = parsed
		}
	}

	days := make(map[string]recordingDay)
	for _, source := range selected {
		if err := s.scanRecordingDays(source, days, limit); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.logger.Warn("recording days scan failed", "source", source.ID, "error", err)
		}
	}
	out := make([]recordingDay, 0, len(days))
	for _, day := range days {
		out = append(out, day)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date > out[j].Date })
	if len(out) > limit {
		out = out[:limit]
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) recordingFile(w http.ResponseWriter, r *http.Request) {
	item, err := s.recordingByID(strings.TrimSpace(r.PathValue("id")))
	if err != nil || item.Pending {
		http.NotFound(w, r)
		return
	}
	file, info, err := openRegularRecording(item.absolute)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "video/x-matroska")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(item.absolute)))
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, filepath.Base(item.absolute), info.ModTime(), file)
}

func (s *Server) recordingVideo(w http.ResponseWriter, r *http.Request) {
	if s.cfg.FFmpegPath == "" {
		writeError(w, http.StatusServiceUnavailable, "la reproducción web requiere FFmpeg")
		return
	}
	item, err := s.recordingByID(strings.TrimSpace(r.PathValue("id")))
	if err != nil || item.Pending {
		http.NotFound(w, r)
		return
	}
	start, err := parsePlaybackStart(r.URL.Query().Get("start"), time.Duration(item.DurationSeconds*float64(time.Second)))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.streamRecordingMP4(w, r, item.absolute, start, "recording_id", item.ID)
}

func (s *Server) recordingSources() ([]recordingSource, error) {
	knownByFolder := make(map[string]recordingSource)
	for _, camera := range s.cameras.Cameras() {
		folder := recordingFolderPart(camera.FolderName)
		if folder == "" {
			continue
		}
		knownByFolder[folder] = recordingSource{ID: camera.ID, CameraID: camera.ID, Name: camera.Name, FolderName: folder}
	}

	entries, err := os.ReadDir(s.cfg.RecordingsDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		folder := entry.Name()
		if _, ok := knownByFolder[folder]; ok {
			continue
		}
		knownByFolder[folder] = recordingSource{
			ID:         archivedSourceID(folder),
			Name:       folder + " (archivada)",
			FolderName: folder,
			Archived:   true,
		}
	}

	out := make([]recordingSource, 0, len(knownByFolder))
	for _, source := range knownByFolder {
		out = append(out, source)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Archived != out[j].Archived {
			return !out[i].Archived
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func selectRecordingSources(sources []recordingSource, selectedID string) ([]recordingSource, error) {
	if selectedID == "" {
		return sources, nil
	}
	for _, source := range sources {
		if source.ID == selectedID {
			return []recordingSource{source}, nil
		}
	}
	return nil, errors.New("cámara de grabación no encontrada")
}

func (s *Server) recordingsForSourceDay(source recordingSource, day time.Time) ([]recordingItem, error) {
	dir := filepath.Join(s.cfg.RecordingsDir, source.FolderName, day.Format("2006"), day.Format("01"), day.Format("02"))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		name      string
		absolute  string
		relative  string
		startedAt time.Time
		modified  time.Time
		size      int64
		pending   bool
		recovered bool
	}
	candidates := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		lower := strings.ToLower(entry.Name())
		pending := strings.HasSuffix(lower, ".mkv.partial")
		if !pending && !strings.HasSuffix(lower, ".mkv") {
			continue
		}
		startedAt, ok := recordingStartFromName(day, entry.Name())
		if !ok {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
			continue
		}
		absolute := filepath.Join(dir, entry.Name())
		relative, relErr := filepath.Rel(s.cfg.RecordingsDir, absolute)
		if relErr != nil {
			continue
		}
		if _, ok := safeRecordingPath(s.cfg.RecordingsDir, relative); !ok {
			continue
		}
		candidates = append(candidates, candidate{
			name: entry.Name(), absolute: absolute, relative: filepath.ToSlash(relative), startedAt: startedAt,
			modified: info.ModTime(), size: info.Size(), pending: pending, recovered: strings.Contains(lower, ".recovered.mkv"),
		})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].startedAt.Before(candidates[j].startedAt) })

	defaultDuration := s.cfg.SegmentDuration
	if source.CameraID != "" {
		if camera, ok := s.cameras.Camera(source.CameraID); ok && camera.SegmentDurationSeconds > 0 {
			defaultDuration = time.Duration(camera.SegmentDurationSeconds) * time.Second
		}
	}
	items := make([]recordingItem, 0, len(candidates))
	for index, candidate := range candidates {
		endedAt := candidate.modified
		if candidate.pending {
			endedAt = time.Now()
		}
		if index+1 < len(candidates) && candidates[index+1].startedAt.After(candidate.startedAt) && candidates[index+1].startedAt.Before(endedAt.Add(10*time.Second)) {
			endedAt = candidates[index+1].startedAt
		}
		duration := endedAt.Sub(candidate.startedAt)
		if duration <= 0 || duration > 24*time.Hour {
			duration = defaultDuration
			endedAt = candidate.startedAt.Add(duration)
		}
		id := encodeRecordingID(candidate.relative)
		item := recordingItem{
			ID: id, CameraID: source.CameraID, CameraName: source.Name, FolderName: source.FolderName,
			StartedAt: candidate.startedAt.UTC(), EndedAt: endedAt.UTC(), DurationSeconds: duration.Seconds(),
			Size: candidate.size, Pending: candidate.pending, Recovered: candidate.recovered,
			PlaybackSupported: s.cfg.FFmpegPath != "" && !candidate.pending,
			absolute:          candidate.absolute, relative: candidate.relative,
		}
		if !candidate.pending {
			item.DownloadURL = "/api/recordings/" + url.PathEscape(id) + "/file"
			if item.PlaybackSupported {
				item.PlaybackURL = "/api/recordings/" + url.PathEscape(id) + "/video"
			}
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Server) timelineEvents(day time.Time, recordings []recordingItem) []recordingTimelineEvent {
	dayEnd := day.AddDate(0, 0, 1)
	byCamera := make(map[string][]int)
	for index := range recordings {
		if recordings[index].CameraID != "" {
			byCamera[recordings[index].CameraID] = append(byCamera[recordings[index].CameraID], index)
		}
	}
	out := make([]recordingTimelineEvent, 0)
	for cameraID, indexes := range byCamera {
		for _, event := range s.store.DetectionEventsBetween(cameraID, day.UTC(), dayEnd.UTC(), 5000) {
			created := event.CreatedAt.In(time.Local)
			marker := recordingTimelineEvent{ID: event.ID, Type: event.Type, CreatedAt: event.CreatedAt, DetailURL: "/events/" + url.PathEscape(event.ID)}
			for _, index := range indexes {
				item := &recordings[index]
				start := item.StartedAt.In(time.Local)
				end := item.EndedAt.In(time.Local)
				if !created.Before(start) && created.Before(end.Add(time.Second)) {
					marker.RecordingID = item.ID
					marker.OffsetSecs = created.Sub(start).Seconds()
					item.EventCount++
					break
				}
			}
			out = append(out, marker)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (s *Server) scanRecordingDays(source recordingSource, days map[string]recordingDay, limit int) error {
	root := filepath.Join(s.cfg.RecordingsDir, source.FolderName)
	years, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for yearIndex := len(years) - 1; yearIndex >= 0; yearIndex-- {
		year := years[yearIndex]
		if !safeDateDirectory(year, 4) {
			continue
		}
		months, readErr := os.ReadDir(filepath.Join(root, year.Name()))
		if readErr != nil {
			continue
		}
		for monthIndex := len(months) - 1; monthIndex >= 0; monthIndex-- {
			month := months[monthIndex]
			if !safeDateDirectory(month, 2) {
				continue
			}
			dates, dateErr := os.ReadDir(filepath.Join(root, year.Name(), month.Name()))
			if dateErr != nil {
				continue
			}
			for dateIndex := len(dates) - 1; dateIndex >= 0; dateIndex-- {
				date := dates[dateIndex]
				if !safeDateDirectory(date, 2) {
					continue
				}
				key := year.Name() + "-" + month.Name() + "-" + date.Name()
				if _, parseErr := time.ParseInLocation("2006-01-02", key, time.Local); parseErr != nil {
					continue
				}
				// Entries are returned in lexical order and traversed newest-first.
				// Once this source is older than the oldest retained day, it cannot
				// affect the requested top-N result and the remaining tree is skipped.
				if len(days) >= limit && key < oldestRecordingDay(days) {
					return nil
				}
				files, fileErr := os.ReadDir(filepath.Join(root, year.Name(), month.Name(), date.Name()))
				if fileErr != nil {
					continue
				}
				value := days[key]
				value.Date = key
				for _, file := range files {
					if file.IsDir() || file.Type()&os.ModeSymlink != 0 {
						continue
					}
					lower := strings.ToLower(file.Name())
					if !strings.HasSuffix(lower, ".mkv") && !strings.HasSuffix(lower, ".mkv.partial") {
						continue
					}
					info, infoErr := file.Info()
					if infoErr == nil && info.Mode().IsRegular() && info.Size() > 0 {
						value.Count++
						value.Size += info.Size()
					}
				}
				if value.Count > 0 {
					days[key] = value
					trimRecordingDays(days, limit)
				}
			}
		}
	}
	return nil
}

func safeDateDirectory(entry os.DirEntry, width int) bool {
	return entry.IsDir() && entry.Type()&os.ModeSymlink == 0 && len(entry.Name()) == width && allDigits(entry.Name())
}

func oldestRecordingDay(days map[string]recordingDay) string {
	oldest := ""
	for key := range days {
		if oldest == "" || key < oldest {
			oldest = key
		}
	}
	return oldest
}

func trimRecordingDays(days map[string]recordingDay, limit int) {
	for len(days) > limit {
		delete(days, oldestRecordingDay(days))
	}
}

func (s *Server) recordingByID(id string) (recordingItem, error) {
	relative, err := decodeRecordingID(id)
	if err != nil {
		return recordingItem{}, err
	}
	absolute, ok := safeRecordingPath(s.cfg.RecordingsDir, relative)
	if !ok {
		return recordingItem{}, errors.New("identificador de grabación inválido")
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		return recordingItem{}, os.ErrNotExist
	}
	lower := strings.ToLower(relative)
	if !strings.HasSuffix(lower, ".mkv") && !strings.HasSuffix(lower, ".mkv.partial") {
		return recordingItem{}, errors.New("tipo de grabación inválido")
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) != 5 {
		return recordingItem{}, errors.New("ruta de grabación inválida")
	}
	day, err := time.ParseInLocation("2006/01/02", strings.Join(parts[1:4], "/"), time.Local)
	if err != nil {
		return recordingItem{}, err
	}
	startedAt, ok := recordingStartFromName(day, parts[4])
	if !ok {
		return recordingItem{}, errors.New("nombre de grabación inválido")
	}
	duration := info.ModTime().Sub(startedAt)
	if duration <= 0 || duration > 24*time.Hour {
		duration = s.cfg.SegmentDuration
	}
	return recordingItem{ID: id, StartedAt: startedAt.UTC(), EndedAt: startedAt.Add(duration).UTC(), DurationSeconds: duration.Seconds(), Size: info.Size(), Pending: strings.HasSuffix(lower, ".partial"), absolute: absolute, relative: relative}, nil
}

func parseRecordingDay(raw string) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		now := time.Now()
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local), nil
	}
	day, err := time.ParseInLocation("2006-01-02", raw, time.Local)
	if err != nil {
		return time.Time{}, errors.New("date debe usar el formato YYYY-MM-DD")
	}
	return day, nil
}

func parsePlaybackStart(raw string, duration time.Duration) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	seconds, err := strconv.ParseFloat(raw, 64)
	if err != nil || seconds < 0 {
		return 0, errors.New("start debe ser un número de segundos no negativo")
	}
	start := time.Duration(seconds * float64(time.Second))
	if duration > 0 && start >= duration {
		return 0, errors.New("start está fuera de la duración de la grabación")
	}
	return start, nil
}

func encodeRecordingID(relative string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(filepath.ToSlash(relative)))
}

func decodeRecordingID(id string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil || len(decoded) == 0 || len(decoded) > 4096 {
		return "", errors.New("identificador de grabación inválido")
	}
	return string(decoded), nil
}

func recordingFolderPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var builder strings.Builder
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z', character >= 'A' && character <= 'Z', character >= '0' && character <= '9', character == '-', character == '_':
			builder.WriteRune(character)
		default:
			builder.WriteByte('_')
		}
	}
	return builder.String()
}

func archivedSourceID(folder string) string {
	return "archive:" + base64.RawURLEncoding.EncodeToString([]byte(folder))
}

func allDigits(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return value != ""
}
