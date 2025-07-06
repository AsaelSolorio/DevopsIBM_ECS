package weather_consumer

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq" // PostgreSQL driver
)

// WeatherData represents the weather data structure
type WeatherData struct {
	TemperatureMax float64 `json:"Temperature_max"`
	TemperatureMin float64 `json:"Temperature_min"`
	Humidity       float64 `json:"humidity"`
	City           string  `json:"city"`
	Country        string  `json:"country"`
}

var (
	dataToSend *WeatherData
	apiKey     string  // OpenWeather API key
	db         *sql.DB // PostgreSQL database connection
)

// Initialize environment variables
func initEnv() {
	// Load environment variables from .env file
	err := godotenv.Load(".env")
	if err != nil {
		log.Fatalf("Error loading .env file: %v", err)
	}

	// Retrieve API_KEY from environment variables
	apiKey = os.Getenv("API_KEY")
	if apiKey == "" {
		log.Fatalf("API_KEY environment variable is not set")
	}

	log.Println("Environment variables initialized successfully")
}

// Initialize database connection
func initDB() {
	var err error
	db, err = sql.Open("postgres", fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=require",
		os.Getenv("DB_USERNAME"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_HOST"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_NAME")))

	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		log.Fatalf("Database connection error: %v", err)
	}

	log.Println("Connected to PostgreSQL database")
}

// Append weather data to a JSON file in array format
func storeWeatherDataInDB(data *WeatherData) error {
	query := `INSERT INTO weather_data (temperature_max, temperature_min, humidity, city, country) VALUES ($1, $2, $3, $4, $5)`
	_, err := db.Exec(query, data.TemperatureMax, data.TemperatureMin, data.Humidity, data.City, data.Country)
	if err != nil {
		log.Printf("Error storing weather data in database: %v", err)
		return fmt.Errorf("failed to insert weather data into database: %v", err)
	}

	log.Println("Weather data stored in database successfully")
	return nil
}

// Fetch weather data from OpenWeather API
func getCurrentDataTemp(cityName, countryCode string) error {
	url := fmt.Sprintf("https://api.openweathermap.org/data/2.5/weather?q=%s,%s&appid=%s&units=metric", cityName, countryCode, apiKey)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch weather data: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch weather data: status code %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode weather data: %v", err)
	}

	mainData, ok := result["main"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid weather data format")
	}

	dataToSend = &WeatherData{
		TemperatureMax: mainData["temp_max"].(float64),
		TemperatureMin: mainData["temp_min"].(float64),
		Humidity:       mainData["humidity"].(float64),
		City:           cityName,
		Country:        countryCode,
	}

	// Append the fetched data to the JSON file
	if err := storeWeatherDataInDB(dataToSend); err != nil {
		return err
	}

	log.Printf("Fetched weather data: %+v\n", dataToSend)
	return nil
}

// RunWeatherConsumer starts the Gin server and batch fetch routine
func RunWeatherConsumer() {
	// Initialize environment variables
	initEnv()
	initDB() // Initialize database connection

	// Start batch fetch routine
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()

		for {
			<-ticker.C
			// Fetch weather data for predefined cities
			cities := []struct {
				City    string
				Country string
			}{
				{"guadalajara", "MX"},
			}

			for _, city := range cities {
				if err := getCurrentDataTemp(city.City, city.Country); err != nil {
					log.Printf("Error fetching weather data for %s, %s: %v", city.City, city.Country, err)
				}
			}
		}
	}()

	// Initialize Gin server
	r := gin.Default()

	// Root endpoint
	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok 👍🐍"})
	})

	// Fetch weather data
	r.GET("/fetch/:city/:country", func(c *gin.Context) {
		city := c.Param("city")
		country := c.Param("country")

		if err := getCurrentDataTemp(city, country); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": dataToSend})
	})

	// Start the server
	r.Run(":8080")
}
