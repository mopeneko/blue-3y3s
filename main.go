package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"./opprocessor"
	"github.com/comail/colog"
	_ "github.com/go-sql-driver/mysql"
	"github.com/mopeneko/lineapi"
	"github.com/mopeneko/linethrift"
)

func main() {
	startProgramTime := time.Now()
	db, _ := sql.Open("mysql", "tamaki:"+os.Getenv("MYSQL_PASSWORD")+"@tcp(10.25.96.4:3306)/tamaki?parseTime=true&loc=Asia%2FTokyo")
	defer db.Close()

	client := getClient(db)
	ctx := context.Background()

	initLogger()

	for _, cl := range client {
		p, err := cl.GetProfile(ctx)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Logged in -> %s(%s)\n", p.Mid, p.DisplayName)
	}

	opProcessor := opprocessor.Init(client, ctx, db, startProgramTime)
	go opProcessor.ClearKickedCount()
	opProcessor.Run()
}

func getClient(db *sql.DB) []*linethrift.TalkServiceClient {
	rows, err := db.Query(`SELECT token FROM tokens`)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	client := []*linethrift.TalkServiceClient{}
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			log.Println("error:", err.Error())
		}
		cl, _ := lineapi.NewLineClient(token)
		client = append(client, cl)
	}
	return client
}

func initLogger() {
	colog.SetDefaultLevel(colog.LDebug)
	colog.SetMinLevel(colog.LTrace)
	colog.SetFormatter(&colog.StdFormatter{
		Colors: true,
		Flag:   log.Ldate | log.Ltime | log.Lshortfile,
	})
	colog.Register()
}
