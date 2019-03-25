package cmdprocessor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	cmd "../cmdconst"
	"../utils"
	sigar "github.com/cloudfoundry/gosigar"
	"github.com/mopeneko/linethrift"
)

type CommandProcessor struct {
	Utils            *utils.Utils
	DB               *sql.DB
	Ctx              context.Context
	AllSetting       []string
	StartProgramTime time.Time
}

func Init(u *utils.Utils, db *sql.DB, ctx context.Context, startProgramTime time.Time) *CommandProcessor {
	allSetting := []string{
		cmd.SETTING_NAME,
		cmd.SETTING_ICON,
		cmd.SETTING_URL,
		cmd.SETTING_INVITE,
	}
	return &CommandProcessor{u, db, ctx, allSetting, startProgramTime}
}

func (p *CommandProcessor) isEnabledString(text string) (bool, error) {
	switch text {
	case "オン":
		return true, nil
	case "オフ":
		return false, nil
	default:
		return false, errors.New("Switch string is wrong.")
	}
}

func (p *CommandProcessor) CheckSendSpeed(message *linethrift.Message) {
	cl := p.Utils.GetRandomClient()
	msg := p.Utils.GenerateTextMessage(message.To, "計測中")
	start := time.Now()
	cl.SendMessage(p.Ctx, 0, msg)
	end := time.Now()
	msg.Text = fmt.Sprintf("%f秒", end.Sub(start).Seconds())
	cl.SendMessage(p.Ctx, 0, msg)
}

func (p *CommandProcessor) isAlreadyEnabledProtection(gid string, protectionType string, isEnabled bool) (bool, error) {
	var isAlready bool
	err := p.DB.QueryRow(
		`SELECT exists(SELECT 1 FROM protections WHERE `+protectionType+`protection = ? AND id = ?)`,
		isEnabled, gid,
	).Scan(&isAlready)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	return isAlready, nil
}

func (p *CommandProcessor) buildSettingResultText(setType string, isAlready bool, isEnabled bool) string {
	recvmesg := setType
	if !isAlready {
		recvmesg += "を"
		if isEnabled {
			recvmesg += "オン"
		} else {
			recvmesg += "オフ"
		}
		recvmesg += "にしました🐶💙✨"
	} else {
		recvmesg += "は既に"
		if isEnabled {
			recvmesg += "オン"
		} else {
			recvmesg += "オフ"
		}
		recvmesg += "です💦"
	}

	return recvmesg
}

func (p *CommandProcessor) SwitchURLProtection(message *linethrift.Message, isEnabledText string) {
	isEnabled, _ := p.isEnabledString(isEnabledText)
	cl := p.Utils.GetRandomClient()
	isAlready, err := p.isAlreadyEnabledProtection(message.To, "url", isEnabled)
	if err != nil {
		log.Println("error:", err)
	}
	if !isAlready {
		if isEnabled {
			group, err := cl.GetGroup(p.Ctx, message.To)
			if err != nil {
				log.Println("error:", err.Error())
				return
			}
			if !group.PreventedJoinByTicket {
				group.PreventedJoinByTicket = true
				cl.UpdateGroup(p.Ctx, 0, group)
			}
		}
		_, err = p.DB.Exec(
			`UPDATE protections SET urlprotection = ? WHERE id = ?`,
			isEnabled,
			message.To,
		)
		if err != nil {
			log.Println("error:", err.Error())
			return
		}
	}
	cl.SendMessage(
		p.Ctx, 0,
		p.Utils.GenerateTextMessage(
			message.To,
			p.buildSettingResultText("招待リンク拒否", isAlready, isEnabled),
		),
	)
}

func (p *CommandProcessor) SwitchNameProtection(message *linethrift.Message, isEnabledText string) {
	isEnabled, _ := p.isEnabledString(isEnabledText)
	cl := p.Utils.GetRandomClient()
	isAlready, err := p.isAlreadyEnabledProtection(message.To, "name", isEnabled)
	if err != nil {
		log.Println("error:", err)
	}
	if !isAlready {
		if isEnabled {
			group, err := cl.GetGroup(p.Ctx, message.To)
			if err != nil {
				log.Println("error:", err.Error())
				return
			}
			_, err = p.DB.Exec(
				`UPDATE protections SET name = ? WHERE id = ?`,
				group.Name,
				message.To,
			)
			if err != nil {
				log.Println("error:", err.Error())
				return
			}
		}
		_, err = p.DB.Exec(
			`UPDATE protections SET nameprotection = ? WHERE id = ?`,
			isEnabled,
			message.To,
		)
		if err != nil {
			log.Println("error:", err.Error())
			return
		}
	}
	cl.SendMessage(
		p.Ctx, 0,
		p.Utils.GenerateTextMessage(
			message.To,
			p.buildSettingResultText("グループ名ロック", isAlready, isEnabled),
		),
	)
}

func (p *CommandProcessor) SwitchIconProtection(message *linethrift.Message, isEnabledText string) {
	isEnabled, _ := p.isEnabledString(isEnabledText)
	cl := p.Utils.GetRandomClient()
	isAlready, err := p.isAlreadyEnabledProtection(message.To, "image", isEnabled)
	if err != nil {
		log.Println("error:", err)
	}
	if !isAlready {
		if isEnabled {
			err = p.Utils.DownloadGroupPicture(message.To, "botcache/"+message.To+".jpg")
			if err != nil {
				log.Println("error:", err.Error())
				return
			}
		}
		_, err = p.DB.Exec(
			`UPDATE protections SET imageprotection = ? WHERE id = ?`,
			isEnabled,
			message.To,
		)
		if err != nil {
			log.Println("error:", err.Error())
			return
		}
	}
	cl.SendMessage(
		p.Ctx, 0,
		p.Utils.GenerateTextMessage(
			message.To,
			p.buildSettingResultText("アイコンロック", isAlready, isEnabled),
		),
	)
}

func (p *CommandProcessor) SwitchInviteProtection(message *linethrift.Message, isEnabledText string) {
	isEnabled, _ := p.isEnabledString(isEnabledText)
	cl := p.Utils.GetRandomClient()
	isAlready, err := p.isAlreadyEnabledProtection(message.To, "invite", isEnabled)
	if err != nil {
		log.Println("error:", err)
	}
	if !isAlready {
		_, err = p.DB.Exec(
			`UPDATE protections SET inviteprotection = ? WHERE id = ?`,
			isEnabled,
			message.To,
		)
		if err != nil {
			log.Println("error:", err.Error())
			return
		}
	}
	cl.SendMessage(
		p.Ctx, 0,
		p.Utils.GenerateTextMessage(
			message.To,
			p.buildSettingResultText("招待拒否", isAlready, isEnabled),
		),
	)
}

func (p *CommandProcessor) CheckSetting(message *linethrift.Message) {
	client := p.Utils.GetRandomClient()

	protection := [4]([]byte){}
	var inviterFetched string
	var subAdminFetched sql.NullString

	err := p.DB.QueryRow(
		`SELECT nameprotection, imageprotection, urlprotection, inviteprotection, inviter, subadmin
		FROM protections
		WHERE id = ?`,
		message.To,
	).Scan(
		&protection[0],
		&protection[1],
		&protection[2],
		&protection[3],
		&inviterFetched,
		&subAdminFetched,
	)

	if err != nil {
		log.Println("error:", err.Error())
		client.SendMessage(
			p.Ctx,
			0,
			p.Utils.GenerateTextMessage(
				message.To,
				"エラーが発生しました。",
			),
		)
		return
	}

	protectionText := [4]string{}

	for i, v := range protection {
		if v[0] == 1 {
			protectionText[i] = "オン"
		} else {
			protectionText[i] = "オフ"
		}
	}

	inviter := ""
	subAdmin := ""

	contact, err := p.Utils.Client[0].GetContact(p.Ctx, inviterFetched)
	if err == nil {
		inviter = contact.DisplayName
	} else {
		inviter = "アカウント削除"
	}

	if subAdminFetched.Valid {
		contact, err := p.Utils.Client[0].GetContact(p.Ctx, subAdminFetched.String)
		if err == nil {
			subAdmin = contact.DisplayName
		} else {
			subAdmin = "アカウント削除"
		}
	} else {
		subAdmin = "なし"
	}

	status := ""

	for i, switchText := range protectionText {
		status += p.AllSetting[i] + " -> " + switchText + "\n"
	}
	status += "\n招待者 -> " + inviter + "\nサブ管理者 -> " + subAdmin

	client.SendMessage(
		p.Ctx, 0,
		p.Utils.GenerateTextMessage(
			message.To,
			status,
		),
	)
}

func (p *CommandProcessor) CheckPermission(message *linethrift.Message) {
	hasPermission, status, err := p.Utils.HasPermission(message.From)
	if err != nil {
		log.Println("error:", err)
		return
	}
	recvmesg := ""
	if status != "" {
		if hasPermission {
			recvmesg = "あなたは権限を所持しています。"
		} else {
			recvmesg = "あなたの権限は既に失効されています。"
		}
		recvmesg += fmt.Sprintf("\n\n[有効期限]\n%s", status)
	} else {
		recvmesg = "あなたは権限を所持しておりません。"
	}
	p.Utils.SendMessageWithRandomClient(p.Ctx, message.To, recvmesg)
}

func (p *CommandProcessor) formatMB(v uint64) uint64 {
	return v / 1024 / 1024
}

func (p *CommandProcessor) SendStatus(message *linethrift.Message) {
	now := time.Now()
	diff := now.Sub(p.StartProgramTime)

	loadAvg := sigar.LoadAverage{}
	loadAvg.Get()

	mem := sigar.Mem{}
	mem.Get()

	p.Utils.SendMessageWithRandomClient(
		p.Ctx,
		message.To,
		fmt.Sprintf(
			`[Uptime]
%s

[LoadAverage]
%.2f, %.2f, %.2f

[Memory]
Capacity -> %dMiB
Used -> %dMiB
Free -> %dMiB`,
			diff.String(),
			loadAvg.One, loadAvg.Five, loadAvg.Fifteen,
			p.formatMB(mem.Total), p.formatMB(mem.Used), p.formatMB(mem.Free),
		),
	)
}

func (p *CommandProcessor) CheckKickers(message *linethrift.Message) {
	client := p.Utils.Client[0]
	group, _ := client.GetGroup(p.Ctx, message.To)
	validMids := []string{}
	for _, member := range group.Members {
		if p.Utils.IsBotMid(member.Mid) {
			validMids = append(validMids, member.Mid)
		}
	}
	if len(validMids) == len(p.Utils.Client) {
		p.Utils.SendMessageWithRandomClient(
			p.Ctx,
			message.To,
			"全員います🐶💙✨",
		)
	} else {
		notValidSize := len(p.Utils.Client) - len(validMids)
		p.Utils.Client[0].SendMessage(
			p.Ctx, 0,
			p.Utils.GenerateTextMessage(
				message.To,
				fmt.Sprintf("%d体補充します🐶💙✨", notValidSize),
			),
		)
		notValidClients := []*linethrift.TalkServiceClient{}
		for i, mid := range p.Utils.Mids {
			isValid := false
			for _, validMid := range validMids {
				if validMid == mid {
					isValid = true
				}
			}
			if !isValid {
				notValidClients = append(notValidClients, p.Utils.Client[i])
			}
		}
		ticket, err := client.ReissueGroupTicket(p.Ctx, message.To)
		if err != nil {
			log.Println("error:", err)
			return
		}
		if group.PreventedJoinByTicket {
			group.PreventedJoinByTicket = false
			client.UpdateGroup(p.Ctx, 0, group)
		}
		for _, notValidClient := range notValidClients {
			err = notValidClient.AcceptGroupInvitationByTicket(
				p.Ctx, 0, message.To, ticket,
			)
			if err != nil {
				log.Println("error:", err)
				return
			}
		}
		group.PreventedJoinByTicket = true
		p.Utils.GetRandomClient().UpdateGroup(p.Ctx, 0, group)
	}
}

func (p *CommandProcessor) LeaveBots(message *linethrift.Message) {
	wg := &sync.WaitGroup{}
	for _, client := range p.Utils.Client {
		wg.Add(1)
		go func(x *linethrift.TalkServiceClient) {
			defer wg.Done()
			x.LeaveGroup(p.Ctx, 0, message.To)
		}(client)
	}
	wg.Wait()
}

func (p *CommandProcessor) ChangeSubAdmin(message *linethrift.Message, list map[string]bool) {
	list[message.To] = true
	p.Utils.SendMessageWithRandomClient(
		p.Ctx, message.To,
		"サブ管理者にしたいアカウントの連絡先を送信してください🐶💙✨",
	)
}
