package main

import (
	"flag"
	"fmt"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/synerex/synerex_alpha/api"
	"github.com/synerex/synerex_alpha/api/common"
	"github.com/synerex/synerex_alpha/api/ptransit"
	"github.com/synerex/synerex_alpha/sxutil"
	"google.golang.org/grpc"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
)



var (
	serverAddr = flag.String("server_addr", "127.0.0.1:10000", "The synerex server address in the format of host:port")
	nodesrv    = flag.String("nodesrv", "127.0.0.1:9990", "Node ID Server")
	mqsrv      = flag.String("mqtt_serv", "", "MQTT Server Address host:port")
	mquser      = flag.String("mqtt_user", "", "MQTT Username")
	mqpass      = flag.String("mqtt_pass", "", "MQTT UserID")
	mqtopic      = flag.String("mqtt_topic", "", "MQTT Topic")
	idlist     []uint64
	spMap      map[uint64]*sxutil.SupplyOpts
	mu         sync.Mutex
)

func messageHandler(client *MQTT.Client, msg MQTT.Message) {
	fmt.Printf("TOPIC: %s\n", msg.Topic())
	fmt.Printf("MSG: %s\n", msg.Payload())
}


func subscribe(client MQTT.Client, sub chan<- MQTT.Message) {
	subToken := client.Subscribe(
		*mqtopic,
		0,
		func(client MQTT.Client, msg MQTT.Message) {
			sub <- msg
		})
	if subToken.Wait() && subToken.Error() != nil {
		fmt.Println(subToken.Error())
		os.Exit(1) // todo: should handle MQTT error
	}
}

func convertDocor2PTService(msg *string) (service *ptransit.PTService, argJson string){
	// using docor info
	payloads := strings.Split(*msg,",")
	lat, err := strconv.ParseFloat(payloads[8],64)
	if err != nil {
		log.Printf("Can't convert latitude from `%s`", payloads[8])
	}
	lon, err2 := strconv.ParseFloat(payloads[9],64)
	if err2 != nil {
		log.Printf("Can't convert longitude from `%s`", payloads[9])
	}
	place := common.NewPlace().WithPoint(&common.Point{
		Latitude: lat,
		Longitude: lon,
	})
	vid,_ := strconv.Atoi(payloads[1][4:]) // scrape from "KOTAXX"
	human := payloads[4]
	time := payloads[7]
	state,_ := strconv.ParseInt(payloads[5],10,32)
	accuracy, _ := strconv.ParseFloat(payloads[10],32)
	altitude, _ := strconv.ParseFloat(payloads[11],32)

	angle,_ := strconv.ParseFloat(payloads[12],32)
	speed,_ := strconv.ParseFloat(payloads[13], 32)

	rpm,_ := strconv.ParseFloat(payloads[14], 32)
//	odo,_ := strconv.ParseFloat(payloads[15], 32) // odometry from today's start
	total_odm,_ := strconv.ParseFloat(payloads[16], 32) // odometry from car
	pressure,_ := strconv.ParseFloat(payloads[21], 32)
	temparature,_ := strconv.ParseFloat(payloads[22], 32)
	humidity,_ := strconv.ParseFloat(payloads[23], 32)
	ig_acc, _ := strconv.ParseInt(payloads[34],10,32)


	fuel, _ := strconv.ParseFloat(payloads[35],32)
	gps_speed, _ := strconv.ParseFloat(payloads[41],32)
//	gps_speed, _ := strconv.ParseFloat(payloads[41],32)

	argJson = fmt.Sprintf("tm:%s,hm:%s,st:%d,pre:%.1f,temp:%.1f,hum:%.1f,alt:%.1f,rpm:%.1f,speed:%.1f,acc:%.1f,fuel:%.1f,ig:%d,odm:%.1f",
		time, human, state, pressure, temparature, humidity,altitude, rpm, gps_speed,accuracy,fuel,  ig_acc, total_odm)

	service = &ptransit.PTService{
		VehicleId: int32(vid),
		Angle: float32(angle),
		Speed: int32(speed),
		CurrentLocation: place,
	}

//	log.Printf("msg:%v",*service)
	return service,argJson
}


func handleMQMessage(sclient *sxutil.SMServiceClient, msg *string){

	pts,argJson := convertDocor2PTService(msg)
	smo := sxutil.SupplyOpts{
		Name:  "Ecotan Bus Info",
		PTService: pts,
		JSON: argJson,
	}
	sclient.RegisterSupply(&smo)

}



func main() {

	flag.Parse()
	sxutil.RegisterNodeName(*nodesrv, "EcotanProvider", false)

	go sxutil.HandleSigInt()
	sxutil.RegisterDeferFunction(sxutil.UnRegisterNode)

	var opts []grpc.DialOption
//	wg := sync.WaitGroup{} // for syncing other goroutines

	opts = append(opts, grpc.WithInsecure())
	conn, err := grpc.Dial(*serverAddr, opts...)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}

	client := api.NewSMarketClient(conn)
	sclient := sxutil.NewSMServiceClient(client, api.MarketType_PT_SERVICE,"")

	// MQTT

	if len(*mqsrv) == 0 {
		log.Printf("Should speficy server addresses")
		os.Exit(1)
	}

	mopts := MQTT.NewClientOptions()
	mopts.AddBroker(*mqsrv)
	mopts.SetClientID("synerex-provider")
	mopts.SetUsername(*mquser)
	mopts.SetPassword(*mqpass)

	mclient := MQTT.NewClient(mopts)
	token := mclient.Connect()
	token.Wait()
	merr := token.Error()
	if merr != nil {
		panic(merr)
	}

	sub := make(chan MQTT.Message)
	go subscribe(mclient, sub)  // subscribe MQTT channel

	for {
		select {
			case s := <- sub:
				msg := string(s.Payload())
//				fmt.Printf("\nmsg: %s\n", msg)
				handleMQMessage(sclient, &msg)
		}
	}

	sxutil.CallDeferFunctions() // cleanup!

}
