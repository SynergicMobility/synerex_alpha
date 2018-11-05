package main

// Draft code for Advertisement Service Provider

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync"

	pb "github.com/synerex/synerex_alpha/api"
	smutil "github.com/synerex/synerex_alpha/sxutil"
	"golang.org/x/net/websocket"
	"google.golang.org/grpc"
)

var (
	serverAddr = flag.String("server_addr", "127.0.0.1:10000", "The server address in the format of host:port")
	nodesrv    = flag.String("nodesrv", "127.0.0.1:9990", "Node ID Server")
	idlist     []uint64
	dmMap      map[uint64]*smutil.DemandOpts
	// need to refactor
	ws_conn *websocket.Conn
	pb_sp   *pb.Supply
)

func init() {
	idlist = make([]uint64, 10)
	dmMap = make(map[uint64]*smutil.DemandOpts)
}

// callback for each Supply
func supplyCallback(clt *smutil.SMServiceClient, sp *pb.Supply) {
	// check if supply is match with my demand.
	log.Println("Got ad supply callback")
	// choice is supply for me? or not.
	if clt.IsSupplyTarget(sp, idlist) { //
		pb_sp = sp
		// always select Supply
		// clt.SelectSupply(sp)

		// message to client
		m := message{}
		m.Message = "Supply from ..."
		websocket.JSON.Send(ws_conn, m)
	}
}

func subscribeSupply(client *smutil.SMServiceClient) {
	// ここは goroutine!
	ctx := context.Background() // 必要？
	client.SubscribeSupply(ctx, supplyCallback)
	// comes here if channel closed
}

func addDemand(sclient *smutil.SMServiceClient, nm string) {
	opts := &smutil.DemandOpts{Name: nm}
	id := sclient.RegisterDemand(opts)
	idlist = append(idlist, id)
	dmMap[id] = opts
}

type message struct {
	// the json tag means this will serialize as a lowercased field
	Message string `json:"message"`
	Command string `json:"command"`
	Demand  string `json:"demand"`
	Url     string `json:"url"`
}

func HandleClient(sclient *smutil.SMServiceClient) {

	ClientHandler := func(ws *websocket.Conn) {
		for {
			ws_conn = ws
			// allocate our container struct
			var m message

			// receive a message using the codec
			if err := websocket.JSON.Receive(ws, &m); err != nil {
				log.Println(err)
				break
			}

			log.Println("Received message:", m.Message)

			switch m.Command {
			case "Demand":
				addDemand(sclient, m.Demand)
			case "Select":
				sclient.SelectSupply(pb_sp)
			}

			// send a response
			m2 := message{}
			m2.Message = "Thanks for the message!"
			if err := websocket.JSON.Send(ws, m2); err != nil {
				log.Println(err)
				break
			}
		}
	}

	// http.Handle("/", websocket.Handler(ClientHandler))
	// Allow Cross-Domain
	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		s := websocket.Server{Handler: websocket.Handler(ClientHandler)}
		s.ServeHTTP(w, req)
	})

	err := http.ListenAndServe(":5001", nil)
	if err != nil {
		log.Fatalf("ListenAndServe: " + err.Error())
		return
	}
}

func main() {
	flag.Parse()
	smutil.RegisterNodeName(*nodesrv, "AdProvider", false)

	go smutil.HandleSigInt()
	smutil.RegisterDeferFunction(smutil.UnRegisterNode)

	var opts []grpc.DialOption
	wg := sync.WaitGroup{} // for syncing other goroutines

	opts = append(opts, grpc.WithInsecure()) // only for draft version
	conn, err := grpc.Dial(*serverAddr, opts...)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	smutil.RegisterDeferFunction(func() { conn.Close() })

	client := pb.NewSMarketClient(conn)
	argJson := fmt.Sprintf("{Client:Ad}")
	// create client wrapper
	sclient := smutil.NewSMServiceClient(client, pb.MarketType_AD_SERVICE, argJson)

	wg.Add(1)
	go subscribeSupply(sclient)

	// addDemand(sclient, "Advertise video for 30s woman")

	wg.Add(1)
	go HandleClient(sclient)

	wg.Wait()

	smutil.CallDeferFunctions() // cleanup!
}
