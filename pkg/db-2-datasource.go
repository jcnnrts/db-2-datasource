package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	_ "database/sql"

	db2 "github.com/ibmdb/go_ibm_db"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/datasource"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// newDatasource returns datasource.ServeOpts.
func newDatasource() datasource.ServeOpts {

	log.DefaultLogger.Warn("Creating new Db2 datasource")

	// creates a instance manager for your plugin. The function passed
	// into `NewInstanceManger` is called when the instance is created
	// for the first time or when a datasource configuration changed.
	im := datasource.NewInstanceManager(newDataSourceInstance)

	ds := &Db2Datasource{
		im: im,
	}

	return datasource.ServeOpts{
		QueryDataHandler:   ds,
		CheckHealthHandler: ds,
	}
}

// Db2Datasource is an example datasource used to scaffold
// new datasource plugins with an backend.
type Db2Datasource struct {
	// The instance manager can help with lifecycle management
	// of datasource instances in plugins. It's not a requirements
	// but a best practice that we recommend that you follow.
	im instancemgmt.InstanceManager
}

// QueryData handles multiple queries and returns multiple responses.
// req contains the queries []DataQuery (where each query contains RefID as a unique identifer).
// The QueryDataResponse contains a map of RefID to the response for each query, and each response
// contains Frames ([]*Frame).
func (td *Db2Datasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {

	//Get the instance settingsfor the current instance of the Db2Datasource.
	instance, err := td.im.Get(req.PluginContext)
	if err != nil {
		log.DefaultLogger.Info("Failed getting PluginContext")
		return nil, nil
	}

	instSetting, ok := instance.(*instanceSettings)
	if !ok {
		log.DefaultLogger.Info("Failed getting instance settings")
		return nil, nil
	}

	//Open DB
	db := instSetting.pool.Open(instSetting.constr, "SetConnMaxLifetime=90")
	defer db.Close()

	log.DefaultLogger.Info("QueryData() - " + instSetting.name)

	response := backend.NewQueryDataResponse()

	// Loop over queries and execute them individually.
	for _, q := range req.Queries {
		res := td.query(ctx, db, q)

		// Save the response in a hashmap based on with RefID as identifier
		response.Responses[q.RefID] = res
	}

	return response, nil
}

//Query model consists of nothing but a raw query.
type queryModel struct {
	Hide      bool   `json:"hide"`
	QueryText string `json:"queryText"`
}

func (td *Db2Datasource) query(ctx context.Context, db *db2.DBP, query backend.DataQuery) backend.DataResponse {
	//Prepare response objects.
	response := backend.DataResponse{}
	frame := data.NewFrame("response")

	// Unmarshal the json into our queryModel.
	var qm queryModel
	response.Error = json.Unmarshal(query.JSON, &qm)
	if response.Error != nil {
		return response
	}

	//If query is hidden we don't have to show it.
	if qm.Hide == true {
		return response
	}

	// Run the query
	rows, err := db.Query(qm.QueryText)
	defer rows.Close()

	if err != nil {
		log.DefaultLogger.Info("Query() - Failed running query")
		log.DefaultLogger.Warn(err.Error())
		return response
	}

	//Get names of columns, they will be used as names for the series.
	colNames, err := rows.Columns()
	if err != nil {
		log.DefaultLogger.Warn("Query() - Failed to get rows.Columns()")
		return response
	}

	//We use a non-sized slice of pointers to actual variables (in another slice) to get typeless pointers to every column's value in a given row.
	//The values slice will then contain actual usable values that are returned from the database.
	colPtrs := make([]interface{}, len(colNames))
	values := make([]int64, len(colNames)-1)

	var timeColumn time.Time   //Single time value to receive first column of scanned row in.
	var timeSeries []time.Time //Slice to save those single values from each row.

	dataSeriesMap := make(map[int][]int64) //This map has a slice of int64's for each column, except the first (timeSeries) time column.

	//First column is the time column.
	colPtrs[0] = &timeColumn
	// Other columns are always int64.
	for i := range colNames[1:] {
		colPtrs[i+1] = &values[i]
	}

	//Go over each row in the resultset and add its values to the timeseries and the dataseriesMap.
	for rows.Next() {
		err = rows.Scan(colPtrs...)

		if err != nil {
			log.DefaultLogger.Warn("Query() - Failed to do rows.Scan()")
			log.DefaultLogger.Warn(err.Error())
			return response
		}

		timeSeries = append(timeSeries, timeColumn)

		for i, value := range values {
			dataSeriesMap[i] = append(dataSeriesMap[i], value)
		}

	}

	//Build the response.
	//Hardcode the timeseries.
	frame.Fields = append(frame.Fields, data.NewField(colNames[0], nil, timeSeries))

	//Itterate over the rest of the columns.
	for i, name := range colNames[1:] {
		frame.Fields = append(frame.Fields, data.NewField(name, nil, dataSeriesMap[i]))
	}

	response.Frames = append(response.Frames, frame)

	return response
}

// CheckHealth handles health checks sent from Grafana to the plugin.
// The main use case for these health checks is the test button on the
// datasource configuration page which allows users to verify that
// a datasource is working as expected.
func (td *Db2Datasource) CheckHealth(ctx context.Context, req *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	var status = backend.HealthStatusOk
	var message = "MESSAGE NOT SET YET"

	instance, err := td.im.Get(req.PluginContext)
	if err != nil {
		log.DefaultLogger.Info("Failed getting PluginContext")
		return nil, nil
	}

	instSetting, ok := instance.(*instanceSettings)
	if !ok {
		log.DefaultLogger.Info("Failed getting instance settings")
		return nil, nil
	}

	log.DefaultLogger.Warn("Checkhealth() fired")

	db := instSetting.pool.Open(instSetting.constr, "SetConnMaxLifetime=60")
	st, err := db.Prepare("select current timestamp from sysibm.sysdummy1")

	if err != nil {
		log.DefaultLogger.Warn("CheckHealth - Failed on prepare")
		log.DefaultLogger.Warn(err.Error())
	}

	log.DefaultLogger.Warn("CheckHealth - about to run query")
	rows, err := st.Query()

	if err != nil {
		log.DefaultLogger.Warn("CheckHealth - error running query")
		log.DefaultLogger.Warn(err.Error())
	} else {
		if rows != nil {
			log.DefaultLogger.Warn("CheckHealth - getting columns")
			cols, err := rows.Columns()

			if err != nil {
				log.DefaultLogger.Warn("CheckHealth - error getting columns")
				log.DefaultLogger.Warn(err.Error())
			} else {
				log.DefaultLogger.Warn(cols[0])

				for rows.Next() {
					var tme string

					err := rows.Scan(&tme)
					if err != nil {
						log.DefaultLogger.Warn("CheckHealth - error scanning rows")
						log.DefaultLogger.Warn(err.Error())
					} else {
						log.DefaultLogger.Warn("Current time " + tme)
						message = "Check succesful; current timestamp = " + tme
					}

					rows.Close()
				}
			}
		}
	}

	db.Close()
	log.DefaultLogger.Warn("CheckHealth - db closed")

	return &backend.CheckHealthResult{
		Status:  status,
		Message: message,
	}, nil

}

type instanceSettings struct {
	pool   db2.Pool
	constr string
	name   string
}

type myDataSourceOptions struct {
	Host     string
	Port     string
	Database string
	User     string
}

//InstanceFactoryFunc implementation.
func newDataSourceInstance(setting backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	log.DefaultLogger.Warn("newDataSourceInstance()", "data", setting.JSONData)

	// Initialize the Db2 connection pool.
	pl := db2.Pconnect("PoolSize=100")

	// Unload the unsecured JSON data in a myDataSourceOptions struct.
	var dso myDataSourceOptions

	err := json.Unmarshal(setting.JSONData, &dso)
	if err != nil {
		log.DefaultLogger.Warn("error marshaling", "err", err)
		return nil, err
	}

	//Fetch the password from the secured JSON conainer.
	password, _ := setting.DecryptedSecureJSONData["password"]

	constr := fmt.Sprintf("HOSTNAME=%s;PORT=%s;DATABASE=%s;UID=%s;PWD=%s", dso.Host, dso.Port, dso.Database, dso.User, password)

	return &instanceSettings{
		pool:   *pl,
		constr: constr,
		name:   setting.Name,
	}, nil
}

func (s *instanceSettings) Dispose() {
	// Called before creatinga a new instance to allow plugin authors
	// to cleanup.
}
