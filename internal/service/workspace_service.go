package service

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"taskmanager/internal/models"
)

type WorkspaceService struct {
	db *gorm.DB
}

func NewWorkspaceService(db *gorm.DB) *WorkspaceService {
	return &WorkspaceService{db: db}
}

// WorkspaceInfo — workspace kèm vai trò của user đang hỏi.
type WorkspaceInfo struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// Member — thành viên workspace, shape {ID, Name} để frontend dùng chung
// với mọi chỗ đang hiển thị "nhân sự".
type Member struct {
	ID     uint   `json:"ID"`
	Name   string `json:"Name"`
	Role   string `json:"role"`
	Locked bool   `json:"locked"`
}

// Create tạo workspace, gán owner làm thành viên đầu tiên. Workspace ĐẦU TIÊN
// của toàn hệ thống sẽ "nhận nuôi" dữ liệu cũ chưa thuộc workspace nào
// (task/settings tạo từ trước khi có tính năng đăng nhập).
func (s *WorkspaceService) Create(name string, ownerID uint) (models.Workspace, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return models.Workspace{}, fmt.Errorf("tên workspace không được để trống")
	}

	var total int64
	if err := s.db.Model(&models.Workspace{}).Count(&total).Error; err != nil {
		return models.Workspace{}, err
	}

	ws := models.Workspace{Name: name, OwnerID: ownerID}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&ws).Error; err != nil {
			return err
		}
		if err := tx.Create(&models.WorkspaceMember{
			WorkspaceID: ws.ID, UserID: ownerID, Role: "owner",
		}).Error; err != nil {
			return err
		}
		if total == 0 {
			// Nhận nuôi dữ liệu legacy (workspace_id = 0).
			if err := tx.Model(&models.Task{}).Where("workspace_id = 0").
				Update("workspace_id", ws.ID).Error; err != nil {
				return err
			}
			if err := tx.Model(&models.Settings{}).Where("workspace_id = 0").
				Update("workspace_id", ws.ID).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return ws, err
}

// ListForUser trả về các workspace user là thành viên.
func (s *WorkspaceService) ListForUser(userID uint) ([]WorkspaceInfo, error) {
	var infos []WorkspaceInfo
	err := s.db.Model(&models.WorkspaceMember{}).
		Select("workspaces.id, workspaces.name, workspace_members.role").
		Joins("JOIN workspaces ON workspaces.id = workspace_members.workspace_id").
		Where("workspace_members.user_id = ?", userID).
		Order("workspaces.id").
		Scan(&infos).Error
	return infos, err
}

func (s *WorkspaceService) IsMember(wsID, userID uint) (bool, error) {
	var n int64
	err := s.db.Model(&models.WorkspaceMember{}).
		Where("workspace_id = ? AND user_id = ?", wsID, userID).Count(&n).Error
	return n > 0, err
}

// Members trả về thành viên (sort theo username) — đây chính là "team".
func (s *WorkspaceService) Members(wsID uint) ([]Member, error) {
	var members []Member
	err := s.db.Model(&models.WorkspaceMember{}).
		Select("users.id as id, users.username as name, workspace_members.role, workspace_members.locked").
		Joins("JOIN users ON users.id = workspace_members.user_id").
		Where("workspace_members.workspace_id = ?", wsID).
		Order("users.username").
		Scan(&members).Error
	return members, err
}

// IsLocked báo user có đang bị khóa trong workspace không.
// Không phải thành viên → coi như không khóa (membership check ở nơi khác).
func (s *WorkspaceService) IsLocked(wsID, userID uint) (bool, error) {
	var m models.WorkspaceMember
	err := s.db.Where("workspace_id = ? AND user_id = ?", wsID, userID).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return m.Locked, nil
}

// SetMemberLock khóa/mở khóa một thành viên: chỉ owner được thao tác,
// không khóa được owner. Thành viên nhận notification về thay đổi.
func (s *WorkspaceService) SetMemberLock(wsID, actorID, targetID uint, locked bool) error {
	var actor models.WorkspaceMember
	if err := s.db.Where("workspace_id = ? AND user_id = ?", wsID, actorID).First(&actor).Error; err != nil {
		return fmt.Errorf("bạn không phải thành viên workspace này")
	}
	if actor.Role != "owner" {
		return fmt.Errorf("chỉ owner mới được khóa/mở khóa thành viên")
	}

	var target models.WorkspaceMember
	if err := s.db.Where("workspace_id = ? AND user_id = ?", wsID, targetID).First(&target).Error; err != nil {
		return fmt.Errorf("không tìm thấy thành viên id %d trong workspace", targetID)
	}
	if target.Role == "owner" {
		return fmt.Errorf("không thể khóa owner")
	}
	if target.Locked == locked {
		if locked {
			return fmt.Errorf("thành viên đã bị khóa trước đó")
		}
		return fmt.Errorf("thành viên không bị khóa")
	}

	var ws models.Workspace
	if err := s.db.First(&ws, wsID).Error; err != nil {
		return err
	}

	content := fmt.Sprintf("Bạn đã được mở khóa trong workspace \"%s\" — có thể thao tác trở lại", ws.Name)
	if locked {
		content = fmt.Sprintf("Bạn đã bị khóa trong workspace \"%s\" — không thể thao tác cho tới khi được mở khóa", ws.Name)
	}

	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&target).Update("locked", locked).Error; err != nil {
			return err
		}
		return tx.Create(&models.Notification{
			UserID: targetID, Kind: "info", Content: content,
		}).Error
	})
}

// MemberNames trả về map userID → username của workspace.
func (s *WorkspaceService) MemberNames(wsID uint) (map[uint]string, error) {
	members, err := s.Members(wsID)
	if err != nil {
		return nil, err
	}
	m := make(map[uint]string, len(members))
	for _, mb := range members {
		m[mb.ID] = mb.Name
	}
	return m, nil
}

// Invite mời user (theo username) vào workspace, sinh notification cho họ.
func (s *WorkspaceService) Invite(wsID, inviterID uint, username string, notifs *NotificationService) error {
	username = strings.TrimSpace(strings.ToLower(username))
	var invitee models.User
	err := s.db.Where("username = ?", username).First(&invitee).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("không tìm thấy username %q", username)
	}
	if err != nil {
		return err
	}

	if ok, err := s.IsMember(wsID, invitee.ID); err != nil {
		return err
	} else if ok {
		return fmt.Errorf("%q đã là thành viên workspace", username)
	}
	var pending int64
	if err := s.db.Model(&models.Invitation{}).
		Where("workspace_id = ? AND invitee_id = ? AND status = 'pending'", wsID, invitee.ID).
		Count(&pending).Error; err != nil {
		return err
	}
	if pending > 0 {
		return fmt.Errorf("%q đã có lời mời đang chờ", username)
	}

	var ws models.Workspace
	if err := s.db.First(&ws, wsID).Error; err != nil {
		return err
	}
	var inviter models.User
	if err := s.db.First(&inviter, inviterID).Error; err != nil {
		return err
	}

	return s.db.Transaction(func(tx *gorm.DB) error {
		inv := models.Invitation{
			WorkspaceID: wsID, InviterID: inviterID, InviteeID: invitee.ID, Status: "pending",
		}
		if err := tx.Create(&inv).Error; err != nil {
			return err
		}
		return tx.Create(&models.Notification{
			UserID: invitee.ID, Kind: "invite", InvitationID: &inv.ID,
			Content: fmt.Sprintf("%s mời bạn vào workspace \"%s\"", inviter.Username, ws.Name),
		}).Error
	})
}

// Respond chấp nhận/từ chối lời mời; accept → thêm thành viên; báo lại người mời.
func (s *WorkspaceService) Respond(invID, userID uint, accept bool) error {
	var inv models.Invitation
	if err := s.db.First(&inv, invID).Error; err != nil {
		return fmt.Errorf("không tìm thấy lời mời")
	}
	if inv.InviteeID != userID {
		return fmt.Errorf("lời mời không thuộc về bạn")
	}
	if inv.Status != "pending" {
		return fmt.Errorf("lời mời đã được xử lý trước đó")
	}

	var ws models.Workspace
	if err := s.db.First(&ws, inv.WorkspaceID).Error; err != nil {
		return err
	}
	var invitee models.User
	if err := s.db.First(&invitee, userID).Error; err != nil {
		return err
	}

	status, verb := "declined", "từ chối"
	if accept {
		status, verb = "accepted", "chấp nhận"
	}

	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&inv).Update("status", status).Error; err != nil {
			return err
		}
		if accept {
			if err := tx.Create(&models.WorkspaceMember{
				WorkspaceID: inv.WorkspaceID, UserID: userID, Role: "member",
			}).Error; err != nil {
				return err
			}
		}
		// Đánh dấu đã đọc notification lời mời tương ứng.
		if err := tx.Model(&models.Notification{}).
			Where("invitation_id = ?", inv.ID).Update("read", true).Error; err != nil {
			return err
		}
		// Báo lại cho người mời.
		return tx.Create(&models.Notification{
			UserID: inv.InviterID, Kind: "info",
			Content: fmt.Sprintf("%s đã %s lời mời vào workspace \"%s\"", invitee.Username, verb, ws.Name),
		}).Error
	})
}
