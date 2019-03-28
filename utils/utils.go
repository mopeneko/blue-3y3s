package utils

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"math/big"
	"math/rand"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/mopeneko/lineapi"
	"github.com/mopeneko/linethrift"
)

type Utils struct {
	Client     []*linethrift.TalkServiceClient
	DB         *sql.DB
	Mids       []string
	httpClient *http.Client
}

func Init(client []*linethrift.TalkServiceClient, db *sql.DB) *Utils {
	seed, _ := crand.Int(crand.Reader, big.NewInt(math.MaxInt64))
	rand.Seed(seed.Int64())
	mids := make([]string, len(client))
	for i, cl := range client {
		mids[i] = cl.AuthToken[:33]
	}
	return &Utils{client, db, mids, &http.Client{}}
}

func (p *Utils) GetRandomClient() *linethrift.TalkServiceClient {
	return p.Client[rand.Intn(len(p.Client))]
}

func (p *Utils) GetRandomKicker() *linethrift.TalkServiceClient {
	return p.Client[1:][rand.Intn(len(p.Client)-1)]
}

func (p *Utils) GenerateTextMessage(to string, text string) *linethrift.Message {
	message := linethrift.NewMessage()
	message.To = to
	message.Text = text
	message.ContentType = linethrift.ContentType_NONE
	return message
}

func (p *Utils) HasPermission(mid string) (bool, string, error) {
	var expair mysql.NullTime
	err := p.DB.QueryRow(
		`SELECT expair FROM users WHERE id = ?`,
		mid,
	).Scan(&expair)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, "", nil
		} else {
			return false, "", err
		}
	} else {
		if expair.Valid {
			JST, _ := time.LoadLocation("Asia/Tokyo")
			expair.Time.In(JST)
			now := time.Now()
			ut := now.Unix()
			_, offset := now.Zone()
			day := time.Unix((ut/86400)*86400-int64(offset), 0)
			if expair.Time.After(day) || expair.Time.Equal(day) {
				return true, expair.Time.Format("2006-01-02"), nil
			} else {
				return false, expair.Time.Format("2006-01-02"), nil
			}
		} else {
			return true, "なし", nil
		}
	}
	return false, "", nil
}

func (p *Utils) CleanGroups() {
	rows, err := p.DB.Query(
		`SELECT id FROM protections WHERE inviter IN (SELECT id FROM users WHERE expair < CURDATE());`,
	)
	if err != nil {
		log.Println("error:", err.Error())
		return
	}
	defer rows.Close()
	ctx := context.Background()
	for rows.Next() {
		var gid string
		if err := rows.Scan(&gid); err != nil {
			log.Println("error:", err.Error())
			return
		}
		for _, cl := range p.Client {
			cl.LeaveGroup(ctx, 0, gid)
			time.Sleep(time.Second * 2)
		}
	}
	for _, cl := range p.Client {
		gids, _ := cl.GetGroupIdsInvited(ctx)
		for _, gid := range gids {
			cl.RejectGroupInvitation(ctx, 0, gid)
			time.Sleep(time.Second * 2)
		}
		log.Printf("%d group canceled\n", len(gids))
	}
}

func (p *Utils) HasGroupPermission(gid string, mid string) (bool, error) {
	var hasPermission bool
	err := p.DB.QueryRow(
		`SELECT exists(SELECT 1 FROM protections WHERE id = ? AND (inviter = ? OR subadmin = ?))`,
		gid, mid, mid,
	).Scan(&hasPermission)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		} else {
			return false, err
		}
	}
	return hasPermission, nil
}

func (p *Utils) IsBotMid(mid string) bool {
	for _, realMid := range p.Mids {
		if realMid == mid {
			return true
		}
	}
	return false
}

func (p *Utils) UploadGroupPicture(to string, filePath string) error {
	fieldname := "file"
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	filename := filepath.Base(filePath)
	body := &bytes.Buffer{}
	mp := multipart.NewWriter(body)
	paramsField, err := mp.CreateFormField("params")
	if err != nil {
		return err
	}
	params := fmt.Sprintf(
		"{\"name\": \"%s\", \"ver\": \"1.0\", \"oid\": \"%s\", \"type\": \"image\"}",
		filename,
		to,
	)
	if _, err = paramsField.Write([]byte(params)); err != nil {
		return err
	}
	fWriter, err := mp.CreateFormFile(fieldname, filename)
	if err != nil {
		return err
	}
	_, err = io.Copy(fWriter, file)
	if err != nil {
		return err
	}
	contentType := mp.FormDataContentType()
	err = mp.Close()
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", "https://obs-jp.line-apps.com/talk/g/upload.nhn", body)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", lineapi.USER_AGENT)
	req.Header.Set("X-Line-Application", lineapi.LINE_APP)
	req.Header.Set("X-Line-Access", p.GetRandomClient().AuthToken)
	req.Header.Set("X-Line-Carrier", "51089, 1-0")
	req.Header.Set("Content-Type", contentType)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != 201 {
		return errors.New(fmt.Sprintf("Status code is invalid: %d", resp.StatusCode))
	}
	return nil
}

func (p *Utils) DownloadGroupPicture(gid string, filepath string) error {
	ctx := context.Background()
	group, err := p.Client[0].GetGroup(ctx, gid)
	if err != nil {
		return err
	}
	resp, err := http.Get("http://dl.profile.line.naver.jp/" + group.PictureStatus)
	if err != nil {
		return err
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	defer file.Close()
	file.Write(body)
	return nil
}

func (p *Utils) SendMessageWithRandomClient(ctx context.Context, to string, text string) {
	p.GetRandomClient().SendMessage(
		ctx,
		0,
		p.GenerateTextMessage(
			to,
			text,
		),
	)
}
