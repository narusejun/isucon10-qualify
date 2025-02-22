package main

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	geo "github.com/kellydunn/golang-geo"
	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"
)

const Limit = 20
const NazotteLimit = 50

var db *sqlx.DB
var mySQLConnectionData *MySQLConnectionEnv
var chairSearchCondition ChairSearchCondition
var estateSearchCondition EstateSearchCondition

var lowPricedChair *ChairListResponse
var lowPricedChairMutex sync.RWMutex

var cachedEstates = map[int]Estate{}
var cachedEstatesMutex sync.RWMutex

// chairのfeature -> feature idへのマップ
var chairFeatureMap = map[string]int{}

// estateのfeature -> feature idへのマップ
var estateFeatureMap = map[string]int{}

type InitializeResponse struct {
	Language string `json:"language"`
}

type Chair struct {
	ID          int64  `db:"id" json:"id"`
	Name        string `db:"name" json:"name"`
	Description string `db:"description" json:"description"`
	Thumbnail   string `db:"thumbnail" json:"thumbnail"`
	Price       int64  `db:"price" json:"price"`
	Height      int64  `db:"height" json:"height"`
	Width       int64  `db:"width" json:"width"`
	Depth       int64  `db:"depth" json:"depth"`
	Color       string `db:"color" json:"color"`
	Features    string `db:"features" json:"features"`
	Kind        string `db:"kind" json:"kind"`
	Popularity  int64  `db:"popularity" json:"-"`
	Stock       int64  `db:"stock" json:"-"`
	WidthLevel  int    `db:"width_level" json:"-"`
	HeightLevel int    `db:"height_level" json:"-"`
	DepthLevel  int    `db:"depth_level" json:"-"`
	PriceLevel  int    `db:"price_level" json:"-"`
}

type ChairSearchResponse struct {
	Count  int64   `json:"count"`
	Chairs []Chair `json:"chairs"`
}

type ChairListResponse struct {
	Chairs []Chair `json:"chairs"`
}

// Estate 物件
type Estate struct {
	ID          int64   `db:"id" json:"id"`
	Thumbnail   string  `db:"thumbnail" json:"thumbnail"`
	Name        string  `db:"name" json:"name"`
	Description string  `db:"description" json:"description"`
	Latitude    float64 `db:"latitude" json:"latitude"`
	Longitude   float64 `db:"longitude" json:"longitude"`
	Address     string  `db:"address" json:"address"`
	Rent        int64   `db:"rent" json:"rent"`
	DoorHeight  int64   `db:"door_height" json:"doorHeight"`
	DoorWidth   int64   `db:"door_width" json:"doorWidth"`
	Features    string  `db:"features" json:"features"`
	Popularity  int64   `db:"popularity" json:"-"`
	WidthLevel  int     `db:"width_level" json:"-"`
	HeightLevel int     `db:"height_level" json:"-"`
	RentLevel   int     `db:"rent_level" json:"-"`
}

// EstateSearchResponse estate/searchへのレスポンスの形式
type EstateSearchResponse struct {
	Count   int64    `json:"count"`
	Estates []Estate `json:"estates"`
}

type EstateListResponse struct {
	Estates []Estate `json:"estates"`
}

type Coordinate struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type Coordinates struct {
	Coordinates []Coordinate `json:"coordinates"`
}

type Range struct {
	ID  int64 `json:"id"`
	Min int64 `json:"min"`
	Max int64 `json:"max"`
}

type RangeCondition struct {
	Prefix string   `json:"prefix"`
	Suffix string   `json:"suffix"`
	Ranges []*Range `json:"ranges"`
}

type ListCondition struct {
	List []string `json:"list"`
}

type EstateSearchCondition struct {
	DoorWidth  RangeCondition `json:"doorWidth"`
	DoorHeight RangeCondition `json:"doorHeight"`
	Rent       RangeCondition `json:"rent"`
	Feature    ListCondition  `json:"feature"`
}

type ChairSearchCondition struct {
	Width   RangeCondition `json:"width"`
	Height  RangeCondition `json:"height"`
	Depth   RangeCondition `json:"depth"`
	Price   RangeCondition `json:"price"`
	Color   ListCondition  `json:"color"`
	Feature ListCondition  `json:"feature"`
	Kind    ListCondition  `json:"kind"`
}

type BoundingBox struct {
	// TopLeftCorner 緯度経度が共に最小値になるような点の情報を持っている
	TopLeftCorner Coordinate
	// BottomRightCorner 緯度経度が共に最大値になるような点の情報を持っている
	BottomRightCorner Coordinate
}

type MySQLConnectionEnv struct {
	Host     string
	Port     string
	User     string
	DBName   string
	Password string
}

type RecordMapper struct {
	Record []string

	offset int
	err    error
}

func (r *RecordMapper) next() (string, error) {
	if r.err != nil {
		return "", r.err
	}
	if r.offset >= len(r.Record) {
		r.err = fmt.Errorf("too many read")
		return "", r.err
	}
	s := r.Record[r.offset]
	r.offset++
	return s, nil
}

func (r *RecordMapper) NextInt() int {
	s, err := r.next()
	if err != nil {
		return 0
	}
	i, err := strconv.Atoi(s)
	if err != nil {
		r.err = err
		return 0
	}
	return i
}

func (r *RecordMapper) NextFloat() float64 {
	s, err := r.next()
	if err != nil {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		r.err = err
		return 0
	}
	return f
}

func (r *RecordMapper) NextString() string {
	s, err := r.next()
	if err != nil {
		return ""
	}
	return s
}

func (r *RecordMapper) Err() error {
	return r.err
}

func NewMySQLConnectionEnv() *MySQLConnectionEnv {
	return &MySQLConnectionEnv{
		Host:     getEnv("MYSQL_HOST", "127.0.0.1"),
		Port:     getEnv("MYSQL_PORT", "3306"),
		User:     getEnv("MYSQL_USER", "isucon"),
		DBName:   getEnv("MYSQL_DBNAME", "isuumo"),
		Password: getEnv("MYSQL_PASS", "isucon"),
	}
}

func getEnv(key, defaultValue string) string {
	val := os.Getenv(key)
	if val != "" {
		return val
	}
	return defaultValue
}

// ConnectDB isuumoデータベースに接続する
func (mc *MySQLConnectionEnv) ConnectDB() (*sqlx.DB, error) {
	dsn := ""
	if getEnv("MYSQL_UNIX_DOMAIN_SOCKET", "0") == "1" {
		dsn = fmt.Sprintf("%v:%v@unix(/var/run/mysqld/mysqld.sock)/%v", mc.User, mc.Password, mc.DBName)
	} else {
		dsn = fmt.Sprintf("%v:%v@tcp(%v:%v)/%v", mc.User, mc.Password, mc.Host, mc.Port, mc.DBName)
	}
	return sqlx.Open("mysql", dsn)
}

func init() {
	jsonText, err := ioutil.ReadFile("../fixture/chair_condition.json")
	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}
	json.Unmarshal(jsonText, &chairSearchCondition)

	for i, s := range chairSearchCondition.Feature.List {
		chairFeatureMap[s] = i
	}

	jsonText, err = ioutil.ReadFile("../fixture/estate_condition.json")
	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}
	json.Unmarshal(jsonText, &estateSearchCondition)

	for i, s := range estateSearchCondition.Feature.List {
		estateFeatureMap[s] = i
	}
}

func main() {
	// Echo instance
	e := echo.New()

	echoPProf(e)
	echoLogging(e)

	// Middleware
	e.Use(middleware.Recover())

	// Initialize
	e.POST("/initialize", initialize)

	// Chair Handler
	e.GET("/api/chair/:id", getChairDetail)
	e.POST("/api/chair", postChair)
	e.GET("/api/chair/search", searchChairs)
	e.GET("/api/chair/low_priced", getLowPricedChair)
	e.GET("/api/chair/search/condition", getChairSearchCondition)
	e.POST("/api/chair/buy/:id", buyChair)

	// Estate Handler
	e.GET("/api/estate/:id", getEstateDetail)
	e.POST("/api/estate", postEstate)
	e.GET("/api/estate/search", searchEstates)
	e.GET("/api/estate/low_priced", getLowPricedEstate)
	e.POST("/api/estate/req_doc/:id", postEstateRequestDocument)
	e.POST("/api/estate/nazotte", searchEstateNazotte)
	e.GET("/api/estate/search/condition", getEstateSearchCondition)
	e.GET("/api/recommended_estate/:id", searchRecommendedEstateWithChair)

	mySQLConnectionData = NewMySQLConnectionEnv()

	var err error
	db, err = mySQLConnectionData.ConnectDB()
	if err != nil {
		e.Logger.Fatalf("DB connection failed : %v", err)
	}
	db.SetMaxOpenConns(10)
	defer db.Close()

	if getEnv("ECHO_UNIX_DOMAIN_SOCKET", "0") == "1" {
		// ここからソケット接続設定 ---
		socket_file := "/var/run/app.sock"
		os.Remove(socket_file)

		l, err := net.Listen("unix", socket_file)
		if err != nil {
			e.Logger.Fatal(err)
		}

		// go runユーザとnginxのユーザ（グループ）を同じにすれば777じゃなくてok
		err = os.Chmod(socket_file, 0777)
		if err != nil {
			e.Logger.Fatal(err)
		}

		e.Listener = l
		e.Logger.Fatal(e.Start(""))
	} else {
		// Start server
		serverPort := fmt.Sprintf(":%v", getEnv("SERVER_PORT", "1323"))
		e.Logger.Fatal(e.Start(serverPort))
	}
}

func initialize(c echo.Context) error {
	sqlDir := filepath.Join("..", "mysql", "db")
	paths := []string{
		filepath.Join(sqlDir, "0_Schema.sql"),
		filepath.Join(sqlDir, "1_DummyEstateData.sql"),
		filepath.Join(sqlDir, "2_DummyChairData.sql"),
		filepath.Join(sqlDir, "3_estate_feature.sql"),
		filepath.Join(sqlDir, "4_chair_feature.sql"),
	}

	for _, p := range paths {
		sqlFile, _ := filepath.Abs(p)
		cmdStr := fmt.Sprintf("mysql -h %v -u %v -p%v -P %v %v < %v",
			mySQLConnectionData.Host,
			mySQLConnectionData.User,
			mySQLConnectionData.Password,
			mySQLConnectionData.Port,
			mySQLConnectionData.DBName,
			sqlFile,
		)
		if err := exec.Command("bash", "-c", cmdStr).Run(); err != nil {
			c.Logger().Errorf("Initialize script error : %v", err)
			return c.NoContent(http.StatusInternalServerError)
		}
	}

	// isuumo.estate_feature テーブルを構築
	// {
	// 	var arr []struct {
	// 		ID       int    `db:"id"`
	// 		Features string `db:"features"`
	// 	}
	// 	err := db.Select(&arr, "SELECT id, features FROM estate")
	// 	if err != nil {
	// 		c.Logger().Errorf("Initialize script error : %v", err)
	// 		return c.NoContent(http.StatusInternalServerError)
	// 	}
	//
	// 	for _, estate := range arr {
	// 		for _, f := range strings.Split(estate.Features, ",") {
	// 			if len(f) == 0 {
	// 				continue
	// 			}
	//
	// 			if _, err := db.Exec("INSERT INTO estate_feature (estate_id, feature_id) VALUES (?, ?)", estate.ID, estateFeatureMap[f]); err != nil {
	// 				c.Logger().Errorf("Initialize script error : %v", err)
	// 				return c.NoContent(http.StatusInternalServerError)
	// 			}
	// 		}
	// 	}
	// }

	// isuumo.chair_feature テーブルを構築
	// {
	// 	var arr []struct {
	// 		ID       int    `db:"id"`
	// 		Features string `db:"features"`
	// 	}
	// 	err := db.Select(&arr, "SELECT id, features FROM chair")
	// 	if err != nil {
	// 		c.Logger().Errorf("Initialize script error : %v", err)
	// 		return c.NoContent(http.StatusInternalServerError)
	// 	}
	//
	// 	for _, chair := range arr {
	// 		for _, f := range strings.Split(chair.Features, ",") {
	// 			if len(f) == 0 {
	// 				continue
	// 			}
	//
	// 			if _, err := db.Exec("INSERT INTO chair_feature (chair_id, feature_id) VALUES (?, ?)", chair.ID, chairFeatureMap[f]); err != nil {
	// 				c.Logger().Errorf("Initialize script error : %v", err)
	// 				return c.NoContent(http.StatusInternalServerError)
	// 			}
	// 		}
	// 	}
	// }

	return JSON(c, http.StatusOK, InitializeResponse{
		Language: "go",
	})
}

func getChairDetail(c echo.Context) error {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.Echo().Logger.Errorf("Request parameter \"id\" parse error : %v", err)
		return c.NoContent(http.StatusBadRequest)
	}

	chair := Chair{}
	query := `SELECT * FROM chair WHERE id = ?`
	err = db.Get(&chair, query, id)
	if err != nil {
		if err == sql.ErrNoRows {
			c.Echo().Logger.Infof("requested id's chair not found : %v", id)
			return c.NoContent(http.StatusNotFound)
		}
		c.Echo().Logger.Errorf("Failed to get the chair from id : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	} else if chair.Stock <= 0 {
		c.Echo().Logger.Infof("requested id's chair is sold out : %v", id)
		return c.NoContent(http.StatusNotFound)
	}

	return JSON(c, http.StatusOK, chair)
}

func postChair(c echo.Context) error {
	header, err := c.FormFile("chairs")
	if err != nil {
		c.Logger().Errorf("failed to get form file: %v", err)
		return c.NoContent(http.StatusBadRequest)
	}
	f, err := header.Open()
	if err != nil {
		c.Logger().Errorf("failed to open form file: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	defer f.Close()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		c.Logger().Errorf("failed to read csv: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	var currentPrice int64

	// tx, err := db.Begin()
	// if err != nil {
	// 	c.Logger().Errorf("failed to begin tx: %v", err)
	// 	return c.NoContent(http.StatusInternalServerError)
	// }
	// defer tx.Rollback()
	argPlaces := make([]string, len(records))

	args := make([]interface{}, len(records)*17)
	for idx, row := range records {
		rm := RecordMapper{Record: row}
		id := rm.NextInt()
		name := rm.NextString()
		description := rm.NextString()
		thumbnail := rm.NextString()
		price := rm.NextInt()
		height := rm.NextInt()
		width := rm.NextInt()
		depth := rm.NextInt()
		color := rm.NextString()
		features := rm.NextString()
		kind := rm.NextString()
		popularity := rm.NextInt()
		stock := rm.NextInt()
		if err := rm.Err(); err != nil {
			c.Logger().Errorf("failed to read record: %v", err)
			return c.NoContent(http.StatusBadRequest)
		}
		argPlaces[idx] = "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
		args[idx*17+0] = id
		args[idx*17+1] = name
		args[idx*17+2] = description
		args[idx*17+3] = thumbnail
		args[idx*17+4] = price
		args[idx*17+5] = height
		args[idx*17+6] = width
		args[idx*17+7] = depth
		args[idx*17+8] = color
		args[idx*17+9] = features
		args[idx*17+10] = kind
		args[idx*17+11] = popularity
		args[idx*17+12] = stock

		// width_level
		widthLevel := -1
		switch {
		case width < 80:
			widthLevel = 0
		case width >= 80 && width < 110:
			widthLevel = 1
		case width >= 110 && width < 150:
			widthLevel = 2
		case width >= 150:
			widthLevel = 3
		}
		args[idx*17+13] = widthLevel

		// height_level
		heightLevel := -1
		switch {
		case height < 80:
			heightLevel = 0
		case height >= 80 && height < 110:
			heightLevel = 1
		case height >= 110 && height < 150:
			heightLevel = 2
		case height >= 150:
			heightLevel = 3
		}
		args[idx*17+14] = heightLevel

		// depth_level
		depthLevel := -1
		switch {
		case depth < 80:
			depthLevel = 0
		case depth >= 80 && depth < 110:
			depthLevel = 1
		case depth >= 110 && depth < 150:
			depthLevel = 2
		case depth >= 150:
			depthLevel = 3
		}
		args[idx*17+15] = depthLevel

		// rent_level
		priceLevel := -1
		switch {
		case price < 3000:
			priceLevel = 0
		case price >= 3000 && price < 6000:
			priceLevel = 1
		case price >= 6000 && price < 9000:
			priceLevel = 2
		case price >= 9000 && price < 12000:
			priceLevel = 3
		case price >= 12000 && price < 15000:
			priceLevel = 4
		case price >= 15000:
			priceLevel = 5
		}
		args[idx*17+16] = priceLevel

		// chairs[idx] = &Chair{
		// 	ID:          int64(id),
		// 	Name:        name,
		// 	Description: description,
		// 	Thumbnail:   thumbnail,
		// 	Price:       int64(price),
		// 	Height:      int64(height),
		// 	Width:       int64(width),
		// 	Depth:       int64(depth),
		// 	Color:       color,
		// 	Features:    features,
		// 	Kind:        kind,
		// 	Popularity:  int64(popularity),
		// 	Stock:       int64(stock),
		// }

		// isuumo.chair_featureに追加
		// for _, f := range strings.Split(features, ",") {
		// 	if len(f) == 0 {
		// 		continue
		// 	}
		//
		// 	if _, err := tx.Exec("INSERT INTO chair_feature (chair_id, feature_id) VALUES (?, ?)", id, chairFeatureMap[f]); err != nil {
		// 		c.Logger().Errorf("failed to insert chair: %v", err)
		// 		return c.NoContent(http.StatusInternalServerError)
		// 	}
		// }

		currentPrice = int64(price)
	}
	_, err = db.Exec("INSERT INTO chair(id, name, description, thumbnail, price, height, width, depth, color, features, kind, popularity, stock, width_level, height_level, depth_level, price_level) VALUES "+strings.Join(argPlaces, ","), args...)
	if err != nil {
		c.Logger().Errorf("failed to insert chair: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	lowPricedChairMutex.RLock()
	currentButtom := lowPricedChair.Chairs[len(lowPricedChair.Chairs)-1].Price
	lowPricedChairMutex.RUnlock()

	if currentPrice <= currentButtom {
		lowPricedChairMutex.Lock()
		lowPricedChair = nil
		lowPricedChairMutex.Unlock()
	}

	return c.NoContent(http.StatusCreated)
}

func searchChairs(c echo.Context) error {
	conditions := make([]string, 0)
	params := make([]interface{}, 0)

	if c.QueryParam("priceRangeId") != "" {
		chairPrice, err := getRange(chairSearchCondition.Price, c.QueryParam("priceRangeId"))
		if err != nil {
			c.Echo().Logger.Infof("priceRangeID invalid, %v : %v", c.QueryParam("priceRangeId"), err)
			return c.NoContent(http.StatusBadRequest)
		}
		conditions = append(conditions, "price_level = ?")
		params = append(params, chairPrice.ID)
	}

	if c.QueryParam("heightRangeId") != "" {
		chairHeight, err := getRange(chairSearchCondition.Height, c.QueryParam("heightRangeId"))
		if err != nil {
			c.Echo().Logger.Infof("heightRangeIf invalid, %v : %v", c.QueryParam("heightRangeId"), err)
			return c.NoContent(http.StatusBadRequest)
		}
		conditions = append(conditions, "height_level = ?")
		params = append(params, chairHeight.ID)
	}

	if c.QueryParam("widthRangeId") != "" {
		chairWidth, err := getRange(chairSearchCondition.Width, c.QueryParam("widthRangeId"))
		if err != nil {
			c.Echo().Logger.Infof("widthRangeID invalid, %v : %v", c.QueryParam("widthRangeId"), err)
			return c.NoContent(http.StatusBadRequest)
		}
		conditions = append(conditions, "width_level = ?")
		params = append(params, chairWidth.ID)
	}

	if c.QueryParam("depthRangeId") != "" {
		chairDepth, err := getRange(chairSearchCondition.Depth, c.QueryParam("depthRangeId"))
		if err != nil {
			c.Echo().Logger.Infof("depthRangeId invalid, %v : %v", c.QueryParam("depthRangeId"), err)
			return c.NoContent(http.StatusBadRequest)
		}
		conditions = append(conditions, "depth_level = ?")
		params = append(params, chairDepth.ID)
	}

	if c.QueryParam("kind") != "" {
		conditions = append(conditions, "kind = ?")
		params = append(params, c.QueryParam("kind"))
	}

	if c.QueryParam("color") != "" {
		conditions = append(conditions, "color = ?")
		params = append(params, c.QueryParam("color"))
	}

	if c.QueryParam("features") != "" {
		for _, f := range strings.Split(c.QueryParam("features"), ",") {
			conditions = append(conditions, "features LIKE CONCAT('%', ?, '%')")
			params = append(params, f)
		}
	}

	if len(conditions) == 0 {
		c.Echo().Logger.Infof("Search condition not found")
		return c.NoContent(http.StatusBadRequest)
	}

	conditions = append(conditions, "stock > 0")

	page, err := strconv.Atoi(c.QueryParam("page"))
	if err != nil {
		c.Logger().Infof("Invalid format page parameter : %v", err)
		return c.NoContent(http.StatusBadRequest)
	}

	perPage, err := strconv.Atoi(c.QueryParam("perPage"))
	if err != nil {
		c.Logger().Infof("Invalid format perPage parameter : %v", err)
		return c.NoContent(http.StatusBadRequest)
	}

	searchQuery := "SELECT * FROM chair WHERE "
	countQuery := "SELECT COUNT(*) FROM chair WHERE "
	searchCondition := strings.Join(conditions, " AND ")
	limitOffset := " ORDER BY popularity DESC, id ASC LIMIT ? OFFSET ?"

	var res ChairSearchResponse
	err = db.Get(&res.Count, countQuery+searchCondition, params...)
	if err != nil {
		c.Logger().Errorf("searchChairs DB execution error : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	chairs := getEmptyChairSlice()
	defer releaseChairSlice(chairs)

	params = append(params, perPage, page*perPage)
	err = db.Select(&chairs, searchQuery+searchCondition+limitOffset, params...)
	if err != nil {
		if err == sql.ErrNoRows {
			return JSON(c, http.StatusOK, ChairSearchResponse{Count: 0, Chairs: []Chair{}})
		}
		c.Logger().Errorf("searchChairs DB execution error : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	res.Chairs = chairs

	return JSON(c, http.StatusOK, res)
}

func buyChair(c echo.Context) error {
	m := echo.Map{}
	if err := c.Bind(&m); err != nil {
		c.Echo().Logger.Infof("post buy chair failed : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	_, ok := m["email"].(string)
	if !ok {
		c.Echo().Logger.Info("post buy chair failed : email not found in request body")
		return c.NoContent(http.StatusBadRequest)
	}

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.Echo().Logger.Infof("post buy chair failed : %v", err)
		return c.NoContent(http.StatusBadRequest)
	}

	tx, err := db.Beginx()
	if err != nil {
		c.Echo().Logger.Errorf("failed to create transaction : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	defer tx.Rollback()

	var chair Chair
	err = tx.QueryRowx("SELECT * FROM chair WHERE id = ? AND stock > 0 FOR UPDATE", id).StructScan(&chair)
	if err != nil {
		if err == sql.ErrNoRows {
			c.Echo().Logger.Infof("buyChair chair id \"%v\" not found", id)
			return c.NoContent(http.StatusNotFound)
		}
		c.Echo().Logger.Errorf("DB Execution Error: on getting a chair by id : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	_, err = tx.Exec("UPDATE chair SET stock = stock - 1 WHERE id = ?", id)
	if err != nil {
		c.Echo().Logger.Errorf("chair stock update failed : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	err = tx.Commit()
	if err != nil {
		c.Echo().Logger.Errorf("transaction commit error : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	target := -1
	lowPricedChairMutex.RLock()
	for i, chair := range lowPricedChair.Chairs {
		if chair.ID == int64(id) {
			target = i
			break
		}
	}
	lowPricedChairMutex.RUnlock()

	if target > -1 {
		lowPricedChairMutex.Lock()
		lowPricedChair.Chairs[target].Stock--
		if lowPricedChair.Chairs[target].Stock == 0 {
			lowPricedChair = nil
		}
		lowPricedChairMutex.Unlock()
	}

	return c.NoContent(http.StatusOK)
}

func getChairSearchCondition(c echo.Context) error {
	return JSON(c, http.StatusOK, chairSearchCondition)
}

func getLowPricedChair(c echo.Context) error {
	lowPricedChairMutex.RLock()
	defer lowPricedChairMutex.RUnlock()

	if lowPricedChair == nil {
		chairs := getEmptyChairSlice()
		// defer releaseChairSlice(chairs)

		query := `SELECT * FROM chair WHERE stock > 0 ORDER BY price ASC, id ASC LIMIT ?`
		err := db.Select(&chairs, query, Limit)
		if err != nil {
			if err == sql.ErrNoRows {
				c.Logger().Error("getLowPricedChair not found")
				return JSON(c, http.StatusOK, ChairListResponse{constEmptyChairs})
			}
			c.Logger().Errorf("getLowPricedChair DB execution error : %v", err)
			return c.NoContent(http.StatusInternalServerError)
		}

		lowPricedChair = &ChairListResponse{Chairs: chairs}
	}
	return JSON(c, http.StatusOK, lowPricedChair)
}

func getEstateDetail(c echo.Context) error {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.Echo().Logger.Infof("Request parameter \"id\" parse error : %v", err)
		return c.NoContent(http.StatusBadRequest)
	}

	var estate Estate
	err = db.Get(&estate, "SELECT * FROM estate WHERE id = ?", id)
	if err != nil {
		if err == sql.ErrNoRows {
			c.Echo().Logger.Infof("getEstateDetail estate id %v not found", id)
			return c.NoContent(http.StatusNotFound)
		}
		c.Echo().Logger.Errorf("Database Execution error : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	return JSON(c, http.StatusOK, estate)
}

func getRange(cond RangeCondition, rangeID string) (*Range, error) {
	RangeIndex, err := strconv.Atoi(rangeID)
	if err != nil {
		return nil, err
	}

	if RangeIndex < 0 || len(cond.Ranges) <= RangeIndex {
		return nil, fmt.Errorf("Unexpected Range ID")
	}

	return cond.Ranges[RangeIndex], nil
}

func postEstate(c echo.Context) error {
	header, err := c.FormFile("estates")
	if err != nil {
		c.Logger().Errorf("failed to get form file: %v", err)
		return c.NoContent(http.StatusBadRequest)
	}
	f, err := header.Open()
	if err != nil {
		c.Logger().Errorf("failed to open form file: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	defer f.Close()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		c.Logger().Errorf("failed to read csv: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	tx, err := db.Begin()
	if err != nil {
		c.Logger().Errorf("failed to begin tx: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	defer tx.Rollback()
	argPlaces := make([]string, len(records))
	args := make([]interface{}, len(records)*15)

	fargPlaces := make([]string, 0, 1000)
	fargs := make([]interface{}, 0, 1000)
	for idx, row := range records {
		rm := RecordMapper{Record: row}
		id := rm.NextInt()
		name := rm.NextString()
		description := rm.NextString()
		thumbnail := rm.NextString()
		address := rm.NextString()
		latitude := rm.NextFloat()
		longitude := rm.NextFloat()
		rent := rm.NextInt()
		doorHeight := rm.NextInt()
		doorWidth := rm.NextInt()
		features := rm.NextString()
		popularity := rm.NextInt()
		if err := rm.Err(); err != nil {
			c.Logger().Errorf("failed to read record: %v", err)
			return c.NoContent(http.StatusBadRequest)
		}
		argPlaces[idx] = "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
		args[idx*15+0] = id
		args[idx*15+1] = name
		args[idx*15+2] = description
		args[idx*15+3] = thumbnail
		args[idx*15+4] = address
		args[idx*15+5] = latitude
		args[idx*15+6] = longitude
		args[idx*15+7] = rent
		args[idx*15+8] = doorHeight
		args[idx*15+9] = doorWidth
		args[idx*15+10] = features
		args[idx*15+11] = popularity

		// width_level
		widthLevel := -1
		switch {
		case doorWidth < 80:
			widthLevel = 0
		case doorWidth >= 80 && doorWidth < 110:
			widthLevel = 1
		case doorWidth >= 110 && doorWidth < 150:
			widthLevel = 2
		case doorWidth >= 150:
			widthLevel = 3
		}
		args[idx*15+12] = widthLevel

		// height_level
		heightLevel := -1
		switch {
		case doorHeight < 80:
			heightLevel = 0
		case doorHeight >= 80 && doorHeight < 110:
			heightLevel = 1
		case doorHeight >= 110 && doorHeight < 150:
			heightLevel = 2
		case doorHeight >= 150:
			heightLevel = 3
		}
		args[idx*15+13] = heightLevel

		// rent_level
		rentLevel := -1
		switch {
		case rent < 50000:
			rentLevel = 0
		case rent >= 50000 && rent < 100000:
			rentLevel = 1
		case rent >= 100000 && rent < 150000:
			rentLevel = 2
		case rent >= 150000:
			rentLevel = 3
		}
		args[idx*15+14] = rentLevel

		// isuumo.estate_featureに追加
		for _, f := range strings.Split(features, ",") {
			if len(f) == 0 {
				continue
			}

			fargPlaces = append(fargPlaces, "(?, ?)")
			fargs = append(fargs, id, estateFeatureMap[f])
		}
	}

	_, err = tx.Exec("INSERT INTO estate(id, name, description, thumbnail, address, latitude, longitude, rent, door_height, door_width, features, popularity, width_level, height_level, rent_level) VALUES "+strings.Join(argPlaces, ","), args...)
	if err != nil {
		c.Logger().Errorf("failed to insert estate: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	if _, err := tx.Exec("INSERT INTO estate_feature (estate_id, feature_id) VALUES "+strings.Join(fargPlaces, ","), fargs...); err != nil {
		c.Logger().Errorf("failed to insert estate: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	if err := tx.Commit(); err != nil {
		c.Logger().Errorf("failed to commit tx: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.NoContent(http.StatusCreated)
}

func searchEstates(c echo.Context) error {
	conditions := make([]string, 0)
	params := make([]interface{}, 0)

	searchQuery := "SELECT * FROM estate"
	countQuery := "SELECT COUNT(*) FROM estate"

	if c.QueryParam("doorHeightRangeId") != "" {
		doorHeight, err := getRange(estateSearchCondition.DoorHeight, c.QueryParam("doorHeightRangeId"))
		if err != nil {
			c.Echo().Logger.Infof("doorHeightRangeID invalid, %v : %v", c.QueryParam("doorHeightRangeId"), err)
			return c.NoContent(http.StatusBadRequest)
		}
		conditions = append(conditions, "height_level = ?")
		params = append(params, doorHeight.ID)
	}

	if c.QueryParam("doorWidthRangeId") != "" {
		doorWidth, err := getRange(estateSearchCondition.DoorWidth, c.QueryParam("doorWidthRangeId"))
		if err != nil {
			c.Echo().Logger.Infof("doorWidthRangeID invalid, %v : %v", c.QueryParam("doorWidthRangeId"), err)
			return c.NoContent(http.StatusBadRequest)
		}
		conditions = append(conditions, "width_level = ?")
		params = append(params, doorWidth.ID)
	}

	if c.QueryParam("rentRangeId") != "" {
		estateRent, err := getRange(estateSearchCondition.Rent, c.QueryParam("rentRangeId"))
		if err != nil {
			c.Echo().Logger.Infof("rentRangeID invalid, %v : %v", c.QueryParam("rentRangeId"), err)
			return c.NoContent(http.StatusBadRequest)
		}
		conditions = append(conditions, "rent_level = ?")
		params = append(params, estateRent.ID)
	}

	if c.QueryParam("features") != "" {
		searchQuery = "SELECT id, name, description, thumbnail, address, latitude, longitude, rent, door_height, door_width, features, popularity FROM estate INNER JOIN (SELECT estate_id FROM estate_feature WHERE feature_id IN (:FEATURES) GROUP BY estate_id HAVING COUNT(*) = :FEATURES_NUM ) TMP ON estate.id = TMP.estate_id"
		countQuery = "SELECT COUNT(*) FROM estate INNER JOIN (SELECT estate_id FROM estate_feature WHERE feature_id IN (:FEATURES) GROUP BY estate_id HAVING COUNT(*) = :FEATURES_NUM ) TMP ON estate.id = TMP.estate_id"

		var ids []string
		for _, f := range strings.Split(c.QueryParam("features"), ",") {
			if len(f) == 0 {
				continue
			}

			ids = append(ids, strconv.Itoa(estateFeatureMap[f]))
		}

		searchQuery = strings.ReplaceAll(searchQuery, ":FEATURES_NUM", strconv.Itoa(len(ids)))
		searchQuery = strings.ReplaceAll(searchQuery, ":FEATURES", strings.Join(ids, ","))

		countQuery = strings.ReplaceAll(countQuery, ":FEATURES_NUM", strconv.Itoa(len(ids)))
		countQuery = strings.ReplaceAll(countQuery, ":FEATURES", strings.Join(ids, ","))
	}

	if len(conditions) == 0 && c.QueryParam("features") == "" {
		c.Echo().Logger.Infof("searchEstates search condition not found")
		return c.NoContent(http.StatusBadRequest)
	}

	page, err := strconv.Atoi(c.QueryParam("page"))
	if err != nil {
		c.Logger().Infof("Invalid format page parameter : %v", err)
		return c.NoContent(http.StatusBadRequest)
	}

	perPage, err := strconv.Atoi(c.QueryParam("perPage"))
	if err != nil {
		c.Logger().Infof("Invalid format perPage parameter : %v", err)
		return c.NoContent(http.StatusBadRequest)
	}

	searchCondition := strings.Join(conditions, " AND ")
	limitOffset := " ORDER BY popularity DESC, id ASC LIMIT ? OFFSET ?"

	c.Logger().Info(searchQuery + searchCondition + limitOffset)
	c.Logger().Info(countQuery + searchCondition)

	if len(conditions) > 0 {
		countQuery += " WHERE "
		searchQuery += " WHERE "
	}

	var res EstateSearchResponse
	err = db.Get(&res.Count, countQuery+searchCondition, params...)
	if err != nil {
		c.Logger().Errorf("searchEstates DB execution error : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	estates := getEmptyEstateSlice()
	defer releaseEstateSlice(estates)

	params = append(params, perPage, page*perPage)
	err = db.Select(&estates, searchQuery+searchCondition+limitOffset, params...)
	if err != nil {
		if err == sql.ErrNoRows {
			return JSON(c, http.StatusOK, EstateSearchResponse{Count: 0, Estates: constEmptyEstates})
		}
		c.Logger().Errorf("searchEstates DB execution error : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	res.Estates = estates

	return JSON(c, http.StatusOK, res)
}

func getLowPricedEstate(c echo.Context) error {
	estates := getEmptyEstateSlice()
	defer releaseEstateSlice(estates)

	query := `SELECT * FROM estate ORDER BY rent ASC, id ASC LIMIT ?`
	err := db.Select(&estates, query, Limit)
	if err != nil {
		if err == sql.ErrNoRows {
			c.Logger().Error("getLowPricedEstate not found")
			return JSON(c, http.StatusOK, EstateListResponse{constEmptyEstates})
		}
		c.Logger().Errorf("getLowPricedEstate DB execution error : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	return JSON(c, http.StatusOK, EstateListResponse{Estates: estates})
}

func searchRecommendedEstateWithChair(c echo.Context) error {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.Logger().Infof("Invalid format searchRecommendedEstateWithChair id : %v", err)
		return c.NoContent(http.StatusBadRequest)
	}

	chair := Chair{}
	query := `SELECT * FROM chair WHERE id = ?`
	err = db.Get(&chair, query, id)
	if err != nil {
		if err == sql.ErrNoRows {
			c.Logger().Infof("Requested chair id \"%v\" not found", id)
			return c.NoContent(http.StatusBadRequest)
		}
		c.Logger().Errorf("Database execution error : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	estates := getEmptyEstateSlice()
	defer releaseEstateSlice(estates)

	w := chair.Width
	h := chair.Height
	d := chair.Depth
	query = `SELECT * FROM estate WHERE (door_width >= ? AND door_height >= ?) OR (door_width >= ? AND door_height >= ?) OR (door_width >= ? AND door_height >= ?) OR (door_width >= ? AND door_height >= ?) OR (door_width >= ? AND door_height >= ?) OR (door_width >= ? AND door_height >= ?) ORDER BY popularity DESC, id ASC LIMIT ?`
	err = db.Select(&estates, query, w, h, w, d, h, w, h, d, d, w, d, h, Limit)
	if err != nil {
		if err == sql.ErrNoRows {
			return JSON(c, http.StatusOK, EstateListResponse{constEmptyEstates})
		}
		c.Logger().Errorf("Database execution error : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	return JSON(c, http.StatusOK, EstateListResponse{Estates: estates})
}

func searchEstateNazotte(c echo.Context) error {
	coordinates := Coordinates{}
	err := c.Bind(&coordinates)
	if err != nil {
		c.Echo().Logger.Infof("post search estate nazotte failed : %v", err)
		return c.NoContent(http.StatusBadRequest)
	}

	if len(coordinates.Coordinates) == 0 {
		return c.NoContent(http.StatusBadRequest)
	}

	b := coordinates.getBoundingBox()
	estatesInBoundingBox := getEmptyEstateSlice()
	defer releaseEstateSlice(estatesInBoundingBox)

	query := `SELECT id, latitude, longitude FROM estate WHERE latitude <= ? AND latitude >= ? AND longitude <= ? AND longitude >= ?`
	err = db.Select(&estatesInBoundingBox, query, b.BottomRightCorner.Latitude, b.TopLeftCorner.Latitude, b.BottomRightCorner.Longitude, b.TopLeftCorner.Longitude)
	if err == sql.ErrNoRows {
		c.Echo().Logger.Infof("select * from estate where latitude ...", err)
		return JSON(c, http.StatusOK, EstateSearchResponse{Count: 0, Estates: constEmptyEstates})
	} else if err != nil {
		c.Echo().Logger.Errorf("database execution error : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	polyPoints := getEmptyGeoPointSlice()
	defer releaseGeoPointSlice(polyPoints)

	for _, co := range coordinates.Coordinates {
		polyPoints = append(polyPoints, geo.NewPoint(co.Latitude, co.Longitude))
	}
	poly := geo.NewPolygon(polyPoints)

	estatesInPolygonIDs := getEmptyIntSlice()
	defer releaseIntSlice(estatesInPolygonIDs)

	for _, estate := range estatesInBoundingBox {
		if poly.Contains(geo.NewPoint(estate.Latitude, estate.Longitude)) {
			estatesInPolygonIDs = append(estatesInPolygonIDs, int(estate.ID))
		}
	}

	estatesInPolygon := getEmptyEstateSlice()
	defer releaseEstateSlice(estatesInPolygon)

	if len(estatesInPolygonIDs) == 0 {
		return JSON(c, http.StatusOK, EstateSearchResponse{Estates: estatesInPolygon, Count: 0})
	}

	missingIDs := getEmptyIntSlice()
	defer releaseIntSlice(missingIDs)

	cachedEstatesMutex.RLock()
	for _, id := range estatesInPolygonIDs {
		if data, ok := cachedEstates[id]; ok {
			estatesInPolygon = append(estatesInPolygon, data)
		} else {
			missingIDs = append(missingIDs, id)
		}
	}
	cachedEstatesMutex.RUnlock()

	if len(missingIDs) > 0 {
		missingEstates := getEmptyEstateSlice()
		defer releaseEstateSlice(missingEstates)

		query, args, err := sqlx.In("SELECT * FROM estate WHERE id IN (?)", missingIDs)
		if err != nil {
			c.Logger().Errorf("sqlx.In FAIL!! : %v", err)
			return c.NoContent(http.StatusInternalServerError)
		}

		err = db.Select(&missingEstates, db.Rebind(query), args...)
		if err != nil {
			c.Logger().Errorf("searchChairs DB execution error : %v", err)
			return c.NoContent(http.StatusInternalServerError)
		}

		estatesInPolygon = append(estatesInPolygon, missingEstates...)

		cachedEstatesMutex.Lock()
		for _, estate := range missingEstates {
			cachedEstates[int(estate.ID)] = estate
		}
		cachedEstatesMutex.Unlock()
	}

	sort.Slice(estatesInPolygon, func(i, j int) bool {
		if estatesInPolygon[i].Popularity == estatesInPolygon[j].Popularity {
			return estatesInPolygon[i].ID < estatesInPolygon[j].ID
		}
		return estatesInPolygon[i].Popularity > estatesInPolygon[j].Popularity
	})

	var re EstateSearchResponse
	if len(estatesInPolygon) > NazotteLimit {
		re.Estates = estatesInPolygon[:NazotteLimit]
	} else {
		re.Estates = estatesInPolygon
	}
	re.Count = int64(len(re.Estates))

	return JSON(c, http.StatusOK, re)
}

func postEstateRequestDocument(c echo.Context) error {
	m := echo.Map{}
	if err := c.Bind(&m); err != nil {
		c.Echo().Logger.Infof("post request document failed : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	_, ok := m["email"].(string)
	if !ok {
		c.Echo().Logger.Info("post request document failed : email not found in request body")
		return c.NoContent(http.StatusBadRequest)
	}

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.Echo().Logger.Infof("post request document failed : %v", err)
		return c.NoContent(http.StatusBadRequest)
	}

	estate := Estate{}
	query := `SELECT * FROM estate WHERE id = ?`
	err = db.Get(&estate, query, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return c.NoContent(http.StatusNotFound)
		}
		c.Logger().Errorf("postEstateRequestDocument DB execution error : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.NoContent(http.StatusOK)
}

func getEstateSearchCondition(c echo.Context) error {
	return JSON(c, http.StatusOK, estateSearchCondition)
}

func (cs Coordinates) getBoundingBox() BoundingBox {
	coordinates := cs.Coordinates
	boundingBox := BoundingBox{
		TopLeftCorner: Coordinate{
			Latitude: coordinates[0].Latitude, Longitude: coordinates[0].Longitude,
		},
		BottomRightCorner: Coordinate{
			Latitude: coordinates[0].Latitude, Longitude: coordinates[0].Longitude,
		},
	}
	for _, coordinate := range coordinates {
		if boundingBox.TopLeftCorner.Latitude > coordinate.Latitude {
			boundingBox.TopLeftCorner.Latitude = coordinate.Latitude
		}
		if boundingBox.TopLeftCorner.Longitude > coordinate.Longitude {
			boundingBox.TopLeftCorner.Longitude = coordinate.Longitude
		}

		if boundingBox.BottomRightCorner.Latitude < coordinate.Latitude {
			boundingBox.BottomRightCorner.Latitude = coordinate.Latitude
		}
		if boundingBox.BottomRightCorner.Longitude < coordinate.Longitude {
			boundingBox.BottomRightCorner.Longitude = coordinate.Longitude
		}
	}
	return boundingBox
}

func (cs Coordinates) coordinatesToText() string {
	points := make([]string, 0, len(cs.Coordinates))
	for _, c := range cs.Coordinates {
		points = append(points, fmt.Sprintf("%f %f", c.Latitude, c.Longitude))
	}
	return fmt.Sprintf("'POLYGON((%s))'", strings.Join(points, ","))
}
