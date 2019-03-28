package opprocessor

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"../talkprocessor"
	"../utils"

	"github.com/mopeneko/lineapi"
	"github.com/mopeneko/linethrift"
)

type OpProcessor struct {
	Client           []*linethrift.TalkServiceClient
	Ctx              context.Context
	Poll             *lineapi.PollingManager
	DB               *sql.DB
	Utils            *utils.Utils
	TalkProcessor    *talkprocessor.TalkProcessor
	StartProgramTime time.Time
	Kicker           []*linethrift.TalkServiceClient
	Kicked           map[string]map[string]uint
}

func Init(client []*linethrift.TalkServiceClient, ctx context.Context, db *sql.DB, startProgramTime time.Time) *OpProcessor {
	poll, _ := lineapi.NewPollingManager(client[0])
	u := utils.Init(client, db)
	tp := talkprocessor.Init(u, db, ctx, startProgramTime)
	go tp.ClearExecutedList()
	kicker := make([]*linethrift.TalkServiceClient, len(client)-1)
	copy(kicker, client[1:])
	kicked := map[string]map[string]uint{}
	return &OpProcessor{client, ctx, poll, db, u, tp, startProgramTime, kicker, kicked}
}

func (p *OpProcessor) ClearKickedCount() {
	for {
		time.Sleep(time.Minute * 2)
		p.Kicked = map[string]map[string]uint{}
	}
}

func (p *OpProcessor) Run() {
	p.Poll.SetOperationProcessor(linethrift.OpType_NOTIFIED_INVITE_INTO_GROUP, p.invitedIntoGroup)
	p.Poll.SetOperationProcessor(linethrift.OpType_RECEIVE_MESSAGE, p.receivedMessage)
	p.Poll.SetOperationProcessor(linethrift.OpType_NOTIFIED_UPDATE_GROUP, p.updatedGroup)
	p.Poll.SetOperationProcessor(linethrift.OpType_NOTIFIED_KICKOUT_FROM_GROUP, p.kickedoutFromGroup)
	p.Poll.SetOperationProcessor(linethrift.OpType_NOTIFIED_INVITE_INTO_ROOM, p.invitedIntoRoom)
	p.Poll.StartPolling()
}

func (p *OpProcessor) invitedIntoGroup(operation *linethrift.Operation) {
	if strings.Contains(operation.Param3, p.Client[0].AuthToken[:33]) {
		var isContainsUser bool
		err := p.DB.QueryRow(
			`SELECT exists(SELECT 1 FROM users WHERE id = ?)`,
			operation.Param2,
		).Scan(&isContainsUser)
		if err != nil && err != sql.ErrNoRows {
			log.Println("error:", err.Error())
			return
		}
		if isContainsUser {
			group, _ := p.Client[0].GetGroup(p.Ctx, operation.Param1)
			if len(group.Members) < 493 {
				p.Client[0].AcceptGroupInvitation(p.Ctx, 0, operation.Param1)
				group, err := p.Client[0].GetGroup(p.Ctx, operation.Param1)
				if err != nil {
					log.Println("error: メインアカウント参加失敗")
					return
				}
				if group.PreventedJoinByTicket {
					group.PreventedJoinByTicket = false
					p.Client[0].UpdateGroup(p.Ctx, 0, group)
				}
				ticket, _ := p.Client[0].ReissueGroupTicket(p.Ctx, operation.Param1)
				wg := &sync.WaitGroup{}
				for _, cl := range p.Client[1:] {
					wg.Add(1)
					go func(x *linethrift.TalkServiceClient) {
						defer wg.Done()
						x.AcceptGroupInvitationByTicket(p.Ctx, 0, operation.Param1, ticket)
					}(cl)
				}
				wg.Add(1)
				go func() {
					defer wg.Done()
					p.Client[0].SendMessage(
						p.Ctx,
						0,
						p.Utils.GenerateTextMessage(
							operation.Param1,
							"わんたま〜\n\n"+
								"[概要]\n"+
								"「犬山たまき保護bot」はバーチャルYouTuber 犬山たまきのなりきりグループ保護BOTです。\n\n"+
								"犬山たまき\n"+
								"https://www.youtube.com/channel/UC8NZiqKx6fsDT3AVcMiVFyA\n"+
								"https://twitter.com/norioo_\n\n"+
								"[作者]\n"+
								"のえる\n"+
								"http://line.me/ti/p/%40djv5227g\n\n"+
								"※本BOTは非公式です。",
						),
					)
				}()
				wg.Wait()
				group.PreventedJoinByTicket = true
				err = p.Client[0].UpdateGroup(p.Ctx, 0, group)
				if err != nil {
					log.Printf("error: URL参加拒否失敗。\n%s\n", err.Error())
				}
				var isContainsGroup bool
				err = p.DB.QueryRow(
					`SELECT exists(SELECT 1 FROM protections WHERE id = ?)`,
					operation.Param1,
				).Scan(&isContainsGroup)
				if err != nil && err != sql.ErrNoRows {
					log.Println("error:", err.Error())
					return
				}
				if !isContainsGroup {
					_, err = p.DB.Exec(
						`INSERT INTO protections(id, inviter) VALUES (?, ?)`,
						operation.Param1,
						operation.Param2,
					)
					if err != nil {
						log.Println("error:", err.Error())
					}
				}
				log.Printf("info: Joined -> %s(%s)\n", operation.Param1, group.Name)
			}
		} else {
			p.Client[0].RejectGroupInvitation(p.Ctx, 0, operation.Param1)
		}
	} else if okperm, _ := p.Utils.HasGroupPermission(operation.Param1, operation.Param2); !okperm && !p.Utils.IsBotMid(operation.Param2) {
		var isProtected bool
		err := p.DB.QueryRow(
			`SELECT exists(SELECT 1 FROM protections WHERE inviteprotection = TRUE AND id = ?)`,
			operation.Param1,
		).Scan(&isProtected)
		if err != nil && err != sql.ErrNoRows {
			log.Println("error:", err.Error())
			return
		}
		if isProtected {
			go func() {
				kickerSize := len(p.Kicker)
				kickers := make([]*linethrift.TalkServiceClient, kickerSize)
				for i, kicker := range p.Kicker {
					newKicker, _ := lineapi.NewLineClient(kicker.AuthToken)
					kickers[i] = newKicker
				}
				kicked := strings.Split(operation.Param3, "\x1e")
				i := 0
				for _, target := range kicked {
					if ok, _ := p.Utils.HasGroupPermission(operation.Param1, operation.Param3); !ok {
						kickers[i].CancelGroupInvitation(p.Ctx, 0, operation.Param1, []string{target})
						if kickerSize-1 == i {
							i = 0
							time.Sleep(time.Millisecond * 500)
						} else {
							i++
						}
					}
				}
			}()
		}
	}
}

func (p *OpProcessor) receivedMessage(operation *linethrift.Operation) {
	message := operation.Message
	p.TalkProcessor.Process(message)
}

func (p *OpProcessor) updatedGroup(operation *linethrift.Operation) {
	if !p.Utils.IsBotMid(operation.Param2) {
		groupattr, _ := strconv.Atoi(operation.Param3)
		switch int64(groupattr) {
		case int64(linethrift.GroupAttribute_NAME):
			var isProtected bool
			err := p.DB.QueryRow(
				`SELECT exists(SELECT 1 FROM protections WHERE nameprotection = TRUE AND id = ?)`,
				operation.Param1,
			).Scan(&isProtected)
			if err != nil && err != sql.ErrNoRows {
				log.Println("error:", err.Error())
				return
			}
			if isProtected {
				cl := p.Utils.GetRandomKicker()
				group, err := cl.GetGroup(p.Ctx, operation.Param1)
				if err != nil {
					log.Println("error:", err.Error())
					return
				}
				hasPermission, err := p.Utils.HasGroupPermission(operation.Param1, operation.Param2)
				if err != nil {
					log.Println("error:", err.Error())
				}
				var groupname string
				err = p.DB.QueryRow(
					`SELECT name FROM protections WHERE id = ?`,
					operation.Param1,
				).Scan(&groupname)
				if err != nil {
					log.Println("error:", err.Error())
				}
				if !hasPermission {
					err = cl.KickoutFromGroup(p.Ctx, 0, operation.Param1, []string{operation.Param2})
					if err != nil {
						log.Println("error:", err.Error())
					}
					group.Name = groupname
					err = cl.UpdateGroup(p.Ctx, 0, group)
					if err != nil {
						log.Println("error:", err.Error())
					}
				} else {
					groupnamerune := []rune(group.Name)
					if len(groupnamerune) > 50 {
						group.Name = string(groupnamerune[:50])
					}
					_, err = p.DB.Exec(
						`UPDATE protections SET name = ? WHERE id = ?`,
						group.Name,
						operation.Param1,
					)
					if err != nil {
						log.Printf("error: %s | %s", operation.Param1, err.Error())
					}
				}

			}
		case int64(linethrift.GroupAttribute_PICTURE_STATUS):
			var isProtected bool
			err := p.DB.QueryRow(
				`SELECT exists(SELECT 1 FROM protections WHERE imageprotection = TRUE AND id = ?)`,
				operation.Param1,
			).Scan(&isProtected)
			if err != nil && err != sql.ErrNoRows {
				log.Println("error:", err.Error())
			}
			if isProtected {
				cl := p.Utils.GetRandomKicker()
				if err != nil {
					log.Println("error:", err.Error())
					return
				}
				hasPermission, err := p.Utils.HasGroupPermission(operation.Param1, operation.Param2)
				if err != nil {
					log.Println("error:", err.Error())
				}
				if !hasPermission {
					err = cl.KickoutFromGroup(p.Ctx, 0, operation.Param1, []string{operation.Param2})
					if err != nil {
						log.Println("error:", err.Error())
					}
					err = p.Utils.UploadGroupPicture(operation.Param1, "/cache/"+operation.Param1+".jpg")
					if err != nil {
						log.Println("error:", err.Error())
					}
				} else {
					err = p.Utils.DownloadGroupPicture(operation.Param1, "/cache/"+operation.Param1+".jpg")
					if err != nil {
						log.Printf("error: %s | %s", operation.Param1, err.Error())
					}
				}

			}
		case int64(linethrift.GroupAttribute_PREVENTED_JOIN_BY_TICKET):
			var isProtected bool
			err := p.DB.QueryRow(
				`SELECT exists(SELECT 1 FROM protections WHERE urlprotection = TRUE AND id = ?)`,
				operation.Param1,
			).Scan(&isProtected)
			if err != nil && err != sql.ErrNoRows {
				log.Println("error:", err.Error())
			}
			if isProtected {
				cl := p.Utils.GetRandomKicker()
				hasPermission, err := p.Utils.HasGroupPermission(operation.Param1, operation.Param2)
				if err != nil {
					log.Println("error:", err.Error())
				}
				if !hasPermission {
					err = cl.KickoutFromGroup(p.Ctx, 0, operation.Param1, []string{operation.Param2})
					if err != nil {
						log.Println("error:", err.Error())
					}
					group, err := cl.GetGroup(p.Ctx, operation.Param1)
					if err != nil {
						log.Println("error:", err.Error())
						return
					}
					group.PreventedJoinByTicket = true
					err = cl.UpdateGroup(p.Ctx, 0, group)
					if err != nil {
						log.Println("error:", err.Error())
					}
				}
			}
		}
	}
}

func (p *OpProcessor) shuffle(data []string) {
	n := len(data)
	for i := n - 1; i >= 0; i-- {
		j := rand.Intn(i + 1)
		data[i], data[j] = data[j], data[i]
	}
}

func (p *OpProcessor) getKickerIndex(mid string) (int, error) {
	for i, client := range p.Kicker {
		if client.AuthToken[:33] == mid {
			return i, nil
		}
	}

	return 0, errors.New("client is not contained")
}

func (p *OpProcessor) deleteClient(s []*linethrift.TalkServiceClient, i int) []*linethrift.TalkServiceClient {
	s = append(s[:i], s[i+1:]...)
	n := make([]*linethrift.TalkServiceClient, len(s))
	copy(n, s)
	return n
}

func (p *OpProcessor) kickedoutFromGroup(operation *linethrift.Operation) {
	if !p.Utils.IsBotMid(operation.Param2) {
		if p.Utils.IsBotMid(operation.Param3) {
			if ok, _ := p.Utils.HasGroupPermission(operation.Param1, operation.Param2); !ok {
				clientMid := p.Client[0].AuthToken[:33]
				var kickedClient *linethrift.TalkServiceClient
				if operation.Param3 != clientMid {
					i, err := p.getKickerIndex(operation.Param3)
					if err != nil {
						return
					}
					kickedClient = p.Kicker[i]
					p.deleteClient(p.Kicker, i)
					defer func(p *OpProcessor) {
						p.Kicker = append(p.Kicker, kickedClient)
					}(p)
				} else {
					kickedClient = p.Client[0]
				}
				target := []string{operation.Param2}
				for _, client := range p.Kicker {
					err := client.KickoutFromGroup(p.Ctx, 0, operation.Param1, target)
					if err != nil {
						log.Println("error:", err.Error())
						continue
					}
					ticket, err := client.ReissueGroupTicket(p.Ctx, operation.Param1)
					if err != nil {
						log.Println("error:", err.Error())
						continue
					}
					group, err := client.GetGroupWithoutMembers(p.Ctx, operation.Param1)
					if err != nil {
						log.Println("error:", err.Error())
						continue
					}
					if group.PreventedJoinByTicket {
						group.PreventedJoinByTicket = false
						err = client.UpdateGroup(p.Ctx, 0, group)
						if err != nil {
							log.Println("error:", err.Error())
							continue
						}
					}
					kickedClient.AcceptGroupInvitationByTicket(p.Ctx, 0, operation.Param1, ticket)
					group.PreventedJoinByTicket = true
					_ = client.UpdateGroup(p.Ctx, 0, group)
					break
				}
			} else {
				wg := &sync.WaitGroup{}
				for _, client := range p.Client {
					wg.Add(1)
					go func(x *linethrift.TalkServiceClient) {
						defer wg.Done()
						x.LeaveGroup(p.Ctx, 0, operation.Param1)
					}(client)
				}
				wg.Wait()
			}
		} else if ok, _ := p.Utils.HasGroupPermission(operation.Param1, operation.Param2); !ok {
			if ok, _ := p.Utils.HasGroupPermission(operation.Param1, operation.Param3); ok {
				client := p.Utils.GetRandomKicker()
				client.FindAndAddContactsByMid(
					p.Ctx, 0, operation.Param3,
					linethrift.ContactType_MID,
					"",
				)
				client.KickoutFromGroup(
					p.Ctx, 0, operation.Param1,
					[]string{operation.Param3},
				)
				client.InviteIntoGroup(
					p.Ctx, 0, operation.Param1,
					[]string{operation.Param3},
				)
			} else {
				if _, ok := p.Kicked[operation.Param1]; ok {
					if count, ok := p.Kicked[operation.Param1][operation.Param2]; ok {
						if count == 2 {
							p.Kicked[operation.Param1][operation.Param2] = 0
							client := p.Utils.GetRandomKicker()
							client.KickoutFromGroup(p.Ctx, 0, operation.Param1, []string{operation.Param2})
						} else {
							p.Kicked[operation.Param1][operation.Param2]++
						}
					} else {
						p.Kicked[operation.Param1][operation.Param2] = 1
					}
				} else {
					p.Kicked[operation.Param1] = map[string]uint{
						operation.Param2: 1,
					}
				}
			}
		}
	}
}

func (p *OpProcessor) invitedIntoRoom(operation *linethrift.Operation) {
	wg := &sync.WaitGroup{}
	for _, client := range p.Client {
		wg.Add(1)
		go func(x *linethrift.TalkServiceClient) {
			defer wg.Done()
			x.LeaveRoom(p.Ctx, 0, operation.Param1)
		}(client)
	}
	wg.Wait()
}
