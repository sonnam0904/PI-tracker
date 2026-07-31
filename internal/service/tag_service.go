package service

import (
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"taskmanager/internal/models"
)

// maxTagName giới hạn độ dài tên tag để combobox không bị một dòng dài phá vỡ
// và để tag vẫn đọc được khi in thành cột trong báo cáo.
const maxTagName = 40

type TagService struct {
	db *gorm.DB
}

func NewTagService(db *gorm.DB) *TagService {
	return &TagService{db: db}
}

// tagKey chuẩn hóa tên tag để so trùng: bỏ khoảng trắng hai đầu, gộp khoảng
// trắng giữa, hạ chữ thường. Nhờ vậy "Hạ tầng", " hạ tầng " và "Hạ  tầng" đều
// trỏ về cùng một tag.
func tagKey(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

// normalizeTagName chuẩn hóa + kiểm tra một tên tag, trả về (tên hiển thị, khóa
// so trùng). Dùng chung cho Create và EnsureByNames để hai đường vào không lệch
// luật hợp lệ.
func normalizeTagName(raw string) (string, string, error) {
	name := strings.Join(strings.Fields(raw), " ")
	if name == "" {
		return "", "", fmt.Errorf("tên tag không được để trống")
	}
	if len([]rune(name)) > maxTagName {
		return "", "", fmt.Errorf("tên tag %q quá dài (tối đa %d ký tự)", name, maxTagName)
	}
	return name, tagKey(name), nil
}

// Create tạo một tag mới cho workspace mà không cần gắn vào task nào.
//
// Idempotent: tên đã tồn tại (không phân biệt chữ hoa/thường) thì KHÔNG lỗi mà
// trả về tag cũ kèm created=false — nhờ vậy caller gọi lại nhiều lần vẫn an toàn
// và không bao giờ tạo được hai tag trùng nghĩa.
func (s *TagService) Create(wsID uint, raw string) (models.Tag, bool, error) {
	name, key, err := normalizeTagName(raw)
	if err != nil {
		return models.Tag{}, false, err
	}
	var existing models.Tag
	if err := s.db.Where("workspace_id = ? AND name_key = ?", wsID, key).
		First(&existing).Error; err == nil {
		return existing, false, nil
	}
	t := models.Tag{WorkspaceID: wsID, NameKey: key, Name: name}
	// OnConflict DoNothing + doc lai: hai client tạo cùng tên cùng lúc thì người
	// thua không lỗi, chỉ nhận tag của người thắng (created=false).
	if err := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&t).Error; err != nil {
		return models.Tag{}, false, err
	}
	if t.ID == 0 {
		if err := s.db.Where("workspace_id = ? AND name_key = ?", wsID, key).
			First(&t).Error; err != nil {
			return models.Tag{}, false, err
		}
		return t, false, nil
	}
	return t, true, nil
}

// List trả về toàn bộ tag của workspace, sắp theo tên — đây là danh sách để
// người dùng "chọn lại tag cũ đã tạo".
func (s *TagService) List(wsID uint) ([]models.Tag, error) {
	var tags []models.Tag
	err := s.db.Where("workspace_id = ?", wsID).Order("name").Find(&tags).Error
	return tags, err
}

// EnsureByNames đổi danh sách tên thành danh sách tag ID: tên nào đã có thì
// dùng lại, chưa có thì tạo mới. Đây là chỗ hiện thực yêu cầu "tạo thêm mới
// hoặc chọn lại tag cũ" — frontend chỉ cần gửi tên, không phải quản lý ID.
//
// Trả về ID theo đúng thứ tự tên truyền vào (đã khử trùng theo tagKey).
func (s *TagService) EnsureByNames(wsID uint, names []string) ([]uint, error) {
	type want struct {
		key  string
		name string
	}
	wants := make([]want, 0, len(names))
	seen := make(map[string]bool, len(names))
	for _, raw := range names {
		// Tên rỗng bị bỏ qua (không lỗi) vì đây là đường vào từ form/DTO; còn
		// Create là thao tác tường minh nên tên rỗng phải báo lỗi.
		if strings.TrimSpace(raw) == "" {
			continue
		}
		name, key, err := normalizeTagName(raw)
		if err != nil {
			return nil, err
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		wants = append(wants, want{key: key, name: name})
	}
	if len(wants) == 0 {
		return nil, nil
	}

	keys := make([]string, len(wants))
	for i, w := range wants {
		keys[i] = w.key
	}

	ids := make([]uint, 0, len(wants))
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var existing []models.Tag
		if err := tx.Where("workspace_id = ? AND name_key IN ?", wsID, keys).
			Find(&existing).Error; err != nil {
			return err
		}
		byKey := make(map[string]uint, len(existing))
		for _, t := range existing {
			byKey[t.NameKey] = t.ID
		}
		for _, w := range wants {
			if id, ok := byKey[w.key]; ok {
				ids = append(ids, id)
				continue
			}
			t := models.Tag{WorkspaceID: wsID, NameKey: w.key, Name: w.name}
			// OnConflict DoNothing + đọc lại: hai client tạo cùng tên cùng lúc thì
			// người thua không lỗi, chỉ dùng hàng của người thắng.
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&t).Error; err != nil {
				return err
			}
			if t.ID == 0 {
				if err := tx.Where("workspace_id = ? AND name_key = ?", wsID, w.key).
					First(&t).Error; err != nil {
					return err
				}
			}
			ids = append(ids, t.ID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// NamesByTask trả về map taskID → danh sách TÊN tag (đã sắp), cho cả workspace
// trong một query — dùng ở ListTasks để nhồi vào DTO. Trả về tên (không phải
// ID) vì mọi tầng phía trên (lọc, nhóm, báo cáo) đều làm việc trên tên.
func (s *TagService) NamesByTask(wsID uint) (map[uint][]string, error) {
	return s.namesByTask(wsID, nil)
}

// NamesByTaskIDs như NamesByTask nhưng chỉ join tag của các task được nêu tên —
// dùng khi danh sách task đã bị lọc theo kỳ, để không join cả bảng task_tags của
// workspace chỉ để lấy tag của một phần nhỏ.
func (s *TagService) NamesByTaskIDs(wsID uint, taskIDs []uint) (map[uint][]string, error) {
	if len(taskIDs) == 0 {
		return map[uint][]string{}, nil
	}
	return s.namesByTask(wsID, taskIDs)
}

func (s *TagService) namesByTask(wsID uint, taskIDs []uint) (map[uint][]string, error) {
	var rows []struct {
		TaskID uint
		Name   string
	}
	q := s.db.Model(&models.TaskTag{}).
		Select("task_tags.task_id AS task_id, tags.name AS name").
		Joins("JOIN tags ON tags.id = task_tags.tag_id").
		Where("task_tags.workspace_id = ?", wsID)
	if taskIDs != nil {
		q = q.Where("task_tags.task_id IN ?", taskIDs)
	}
	// KHÔNG ORDER BY trong SQL: thứ tự cần là thứ tự HIỂN THỊ của vài tag trong
	// một task, nên sắp ở Go bằng SortedNames — xem sortTagsPerTask.
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[uint][]string, len(rows))
	for _, r := range rows {
		m[r.TaskID] = append(m[r.TaskID], r.Name)
	}
	sortTagsPerTask(m)
	return m, nil
}

// sortTagsPerTask sắp tên tag của TỪNG task bằng SortedNames.
//
// Trước đây việc này do `ORDER BY tags.name` trong SQL làm, và nó sai theo hai
// cách. Một: thứ tự phụ thuộc collation của DB — sqlite (BINARY) và Postgres
// locale C xếp theo byte nên chữ hoa lên trước ("API Zalo báo cáo hạ tầng"),
// còn Postgres locale en_US.UTF-8 lại xếp khác; cùng một app, đổi DB là đổi
// giao diện. Hai: nhật ký thay đổi tag (tagChangeNote) dùng SortedNames — không
// phân biệt hoa/thường — nên chip trên task và dòng nhật ký của chính task đó
// hiện tag theo hai thứ tự khác nhau ("API Zalo báo cáo hạ tầng" so với
// "API báo cáo hạ tầng Zalo").
//
// Sắp ở Go xử lý cả hai: một quy tắc duy nhất, không phụ thuộc DB. Rẻ hơn nữa
// là khác: DB đang sắp TOÀN BỘ hàng của mọi task trong một lần, còn ở đây là
// vài phần tử mỗi task.
func sortTagsPerTask(m map[uint][]string) {
	for id, names := range m {
		m[id] = SortedNames(names)
	}
}

// NamesOf trả về tên tag của một task (đã sắp) — dùng cho MCP và cho việc ghi
// lịch sử thay đổi khi lưu task.
func (s *TagService) NamesOf(taskID uint) ([]string, error) {
	var names []string
	err := s.db.Model(&models.TaskTag{}).
		Select("tags.name").
		Joins("JOIN tags ON tags.id = task_tags.tag_id").
		Where("task_tags.task_id = ?", taskID).
		Scan(&names).Error
	if err != nil {
		return nil, err
	}
	// Sắp ở Go, cùng quy tắc với NamesByTask — hai đường này đổ vào cùng một chỗ
	// trên giao diện, lệch thứ tự là chip nhảy chỗ khi mở/đóng modal.
	return SortedNames(names), nil
}

// SetForTask thay toàn bộ tag của taskID bằng tagIDs (bỏ 0, khử trùng lặp).
// Ghi trong transaction để không để lại trạng thái dở nếu lỗi giữa chừng.
func (s *TagService) SetForTask(wsID, taskID uint, tagIDs []uint) error {
	clean := make([]uint, 0, len(tagIDs))
	seen := make(map[uint]bool, len(tagIDs))
	for _, id := range tagIDs {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		clean = append(clean, id)
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("task_id = ?", taskID).
			Delete(&models.TaskTag{}).Error; err != nil {
			return err
		}
		for _, id := range clean {
			if err := tx.Create(&models.TaskTag{
				WorkspaceID: wsID, TaskID: taskID, TagID: id,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteForTask xóa mọi liên kết tag của taskID — gọi khi xóa task để không còn
// hàng join mồ côi. Bản thân tag vẫn giữ lại trong workspace (là từ vựng dùng
// chung, không thuộc riêng task nào).
func (s *TagService) DeleteForTask(taskID uint) error {
	return s.db.Where("task_id = ?", taskID).Delete(&models.TaskTag{}).Error
}

// Delete xóa tag khỏi workspace và bỏ nó khỏi mọi task đang gắn.
func (s *TagService) Delete(wsID, tagID uint) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ? AND workspace_id = ?", tagID, wsID).Delete(&models.Tag{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("không tìm thấy tag id %d trong workspace hiện tại", tagID)
		}
		return tx.Where("tag_id = ?", tagID).Delete(&models.TaskTag{}).Error
	})
}

// Fingerprint trả COUNT + MAX(id) tag của workspace để dồn vào dấu vân tay đồng
// bộ realtime. Cần thiết vì XÓA tag không ghi activity nào: nếu không có số này,
// client khác vẫn hiện tag đã bị xóa cho tới lần thay đổi dữ liệu kế tiếp.
// (Tạo tag thì luôn kèm một lần lưu task nên đã được activity bắt.)
func (s *TagService) Fingerprint(wsID uint) (count int64, maxID int64, err error) {
	var row struct {
		N     int64
		MaxID int64
	}
	err = s.db.Model(&models.Tag{}).
		Select("COUNT(*) AS n, COALESCE(MAX(id), 0) AS max_id").
		Where("workspace_id = ?", wsID).
		Scan(&row).Error
	return row.N, row.MaxID, err
}

// SortedNames sắp một danh sách tên tag theo thứ tự không phân biệt chữ hoa —
// dùng để so sánh trước/sau khi ghi lịch sử thay đổi.
func SortedNames(names []string) []string {
	out := append([]string(nil), names...)
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}
