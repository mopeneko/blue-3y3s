package talkprocessor

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"../cmdchecker"
	cmd "../cmdconst"
	"../cmdparser"
	"../cmdprocessor"
	"../utils"
	"github.com/google/uuid"
	"github.com/mopeneko/linethrift"
)

type TalkProcessor struct {
	Utils                *utils.Utils
	DB                   *sql.DB
	Ctx                  context.Context
	Executed             []string
	NormalHelp           string
	SettingHelp          string
	CmdProcessor         *cmdprocessor.CommandProcessor
	StartProgramTime     time.Time
	ChangeSubAdminSwitch map[string]bool
}

const (
	NormalHelp = `このbotのコマンドリストです🐶💙✨


たまき:権限確認	-> 権限の所持状況を確認できます

たまき:速度 -> botのラグを確認できます

たまき:ステータス -> BOTの稼働時間とシステム情報を確認できます


⚠ここから先は招待者か予備管理者に設定されたメンバーしかできないよ⚠


たまき:キッカー確認 -> キッカーがグループに全員いるか確認できます

たまき:退会 -> BOTがグループから退会させられます（設定情報は保持）


☆これらのコマンドは「💙」から始めても反応するよ`

	SettingHelp = `このbotの設定コマンドリストです🐶💙✨
「たまき:ヘルプ」って言うと通常のコマンドリストを見れるよ✨


設定:確認 -> このグループの設定を確認できます


⚠ここから先は招待者か予備管理者に設定されたメンバーしかできないよ⚠


設定:サブ管理者変更 -> グループのサブ管理者（招待者と同等の権限を持つ）を設定できます

設定:グループ名ロック:オン/オフ -> グループ名を固定するか設定できます

設定:アイコンロック:オン/オフ -> グループアイコンを固定するか設定できます

設定:招待リンク拒否:オン/オフ -> URLからグループに入れなくするか設定できます

設定:招待拒否:オン/オフ -> 管理者か予備管理者以外による招待をキャンセルするか設定できます`
)

func Init(u *utils.Utils, db *sql.DB, ctx context.Context, startProgramTime time.Time) *TalkProcessor {
	executed := []string{}
	cmdp := cmdprocessor.Init(u, db, ctx, startProgramTime)
	changeSubAdminSwitch := make(map[string]bool)
	go func() {
		time.Sleep(time.Hour * 1)
		u.CleanGroups()
	}()

	return &TalkProcessor{u, db, ctx, executed, NormalHelp, SettingHelp, cmdp, startProgramTime, changeSubAdminSwitch}
}

func (p *TalkProcessor) ClearExecutedList() {
	for {
		time.Sleep(time.Second * 2)
		p.Executed = nil
	}
}

func contains(s []string, e string) bool {
	for _, v := range s {
		if e == v {
			return true
		}
	}
	return false
}

func (p *TalkProcessor) Process(message *linethrift.Message) {
	switch message.ToType {
	case linethrift.MIDType_GROUP:
		if !contains(p.Executed, message.To) {
			switch message.ContentType {
			case linethrift.ContentType_NONE:
				// Normal commands
				if prefix, ok := cmdchecker.HasPrefixCommand(message.Text, []string{"たまき:", "💙"}); ok {
					commands := cmdparser.ParsePhrases(message.Text, prefix)
					command := cmdparser.ParseCommand(commands)

					if cmdchecker.IsNormalCommand(commands) {
						flag := true

						switch command {
						case cmd.NORMAL_HELP:
							p.Utils.SendMessageWithRandomClient(p.Ctx, message.To, p.NormalHelp)
						case cmd.NORMAL_CHECKSTATUS:
							p.CmdProcessor.SendStatus(message)
						case cmd.NORMAL_CHECKPERMISSION:
							p.CmdProcessor.CheckPermission(message)
						case cmd.NORMAL_CHECKSPEED:
							p.CmdProcessor.CheckSendSpeed(message)
						default:
							flag = false
						}

						if !flag {
							if ok, _ := p.Utils.HasGroupPermission(message.To, message.From); ok {
								flag = true
								switch command {
								case cmd.NORMAL_CHECKKICKERS:
									p.CmdProcessor.CheckKickers(message)
								case cmd.NORMAL_LEAVEBOTS:
									p.CmdProcessor.LeaveBots(message)
								default:
									flag = false
								}
							}
						}

						if flag {
							p.Executed = append(p.Executed, message.To)
						}
					}
					return
				} else

				// Setting commands
				if prefix, ok := cmdchecker.HasPrefixCommand(message.Text, []string{"設定:"}); ok {
					commands := cmdparser.ParsePhrases(message.Text, prefix)
					command := cmdparser.ParseCommand(commands)

					flag := true

					if cmdchecker.IsNormalCommand(commands) {
						switch command {
						case cmd.NORMAL_HELP:
							p.Utils.SendMessageWithRandomClient(p.Ctx, message.To, p.SettingHelp)
						case cmd.SETTING_CHECK:
							p.CmdProcessor.CheckSetting(message)
						default:
							flag = false
						}

						if !flag {
							if ok, _ := p.Utils.HasGroupPermission(message.To, message.From); ok {
								flag = true
								switch command {
								case cmd.NORMAL_CHANGESUBADMIN:
									p.CmdProcessor.ChangeSubAdmin(message, p.ChangeSubAdminSwitch)
								default:
									flag = false
								}
							}
						}
					} else {
						flag = false
					}

					if ok, _ := p.Utils.HasGroupPermission(message.To, message.From); ok && !flag && cmdchecker.IsSettingCommand(commands) {
						flag = true

						isEnabledText := commands[2]

						switch command {
						case cmd.SETTING_NAME:
							p.CmdProcessor.SwitchNameProtection(message, isEnabledText)
						case cmd.SETTING_ICON:
							p.CmdProcessor.SwitchIconProtection(message, isEnabledText)
						case cmd.SETTING_URL:
							p.CmdProcessor.SwitchURLProtection(message, isEnabledText)
						case cmd.SETTING_INVITE:
							p.CmdProcessor.SwitchInviteProtection(message, isEnabledText)
						default:
							flag = false
						}
					}

					if flag {
						p.Executed = append(p.Executed, message.To)
					}

					return
				}

			case linethrift.ContentType_CONTACT:
				if isEnabled, ok := p.ChangeSubAdminSwitch[message.To]; ok {
					if isEnabled {
						if ok, _ := p.Utils.HasGroupPermission(message.To, message.From); ok {
							defer func() {
								p.ChangeSubAdminSwitch[message.To] = false
							}()
							mid := message.ContentMetadata["mid"]
							contact, err := p.Utils.Client[0].GetContact(p.Ctx, mid)
							if err != nil {
								p.Utils.SendMessageWithRandomClient(
									p.Ctx, message.To,
									"エラーが発生しました💦\n連絡先をお確かめください💦💦",
								)
								return
							}
							_, err = p.DB.Exec(
								`UPDATE protections SET subadmin = ? WHERE id = ?`,
								mid, message.To,
							)
							if err != nil {
								p.Utils.SendMessageWithRandomClient(
									p.Ctx, message.To,
									"エラーが発生しました💦\n連絡先をお確かめください💦💦",
								)
								log.Println("error:", err.Error())
								return
							}
							p.Utils.SendMessageWithRandomClient(
								p.Ctx, message.To,
								fmt.Sprintf("%sをサブ管理者に設定しました🐶💙✨", contact.DisplayName),
							)
						}
					}
				}
			}
		}
	case linethrift.MIDType_USER:
		if message.From == "u82e0913834e04d1514f7a071ea38b3aa" {
			if message.Text == "チケット発行" {
				id := uuid.New().String()
				_, err := p.DB.Exec(
					`INSERT INTO tickets(uuid) VALUES(?)`,
					id,
				)
				if err != nil {
					log.Println("error:", err.Error())
				}
				message.To = message.From
				message.Text = fmt.Sprintf("RegiProtect:%s", id)
				p.Utils.Client[0].SendMessage(p.Ctx, 0, message)
			}
		}
	}
}
