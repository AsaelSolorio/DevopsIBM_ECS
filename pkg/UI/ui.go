package UI

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
)

// WeatherData represents the weather data structure
type WeatherData struct {
	TemperatureMax float64 `json:"Temperature_max"`
	TemperatureMin float64 `json:"Temperature_min"`
	Humidity       float64 `json:"humidity"`
	City           string  `json:"city"`
	Country        string  `json:"country"`
}

// ForecastData represents the structure of the forecast data
type ForecastData struct {
	Date              string  `json:"date"`
	ForecastTemp      float64 `json:"forecast_temp"`
	ForecastTempLower float64 `json:"forecast_temp_lower"`
	ForecastTempUpper float64 `json:"forecast_temp_upper"`
}

var (
	apiKey string // OpenWeather API key

	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins for simplicity
		},
	}
	clients = make(map[*websocket.Conn]bool) // Track connected WebSocket clients
)

// Initialize environment variables
func initEnv() {
	err := godotenv.Load(".env")
	if err != nil {
		log.Fatalf("Error loading .env file: %v", err)
	}

	apiKey = os.Getenv("API_KEY")
	if apiKey == "" {
		log.Fatalf("API_KEY environment variable is not set")
	}
}

// Fetch weather data from OpenWeather API
func fetchWeatherData(cityName, countryCode string) (*WeatherData, error) {
	encodedCity := url.QueryEscape(cityName)
	encodedCountry := url.QueryEscape(countryCode)

	url := fmt.Sprintf("https://api.openweathermap.org/data/2.5/weather?q=%s,%s&appid=%s&units=metric", encodedCity, encodedCountry, apiKey)
	log.Printf("Fetching weather data for %s, %s from URL: %s", cityName, countryCode, url)

	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Failed to fetch weather data: %v", err)
		return nil, fmt.Errorf("failed to fetch weather data: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("API returned non-200 status code: %d", resp.StatusCode)
		return nil, fmt.Errorf("failed to fetch weather data: status code %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("Failed to decode weather data: %v", err)
		return nil, fmt.Errorf("failed to decode weather data: %v", err)
	}

	mainData, ok := result["main"].(map[string]interface{})
	if !ok {
		log.Printf("Invalid weather data format: %v", result)
		return nil, fmt.Errorf("invalid weather data format")
	}

	weatherData := &WeatherData{
		TemperatureMax: mainData["temp_max"].(float64),
		TemperatureMin: mainData["temp_min"].(float64),
		Humidity:       mainData["humidity"].(float64),
		City:           cityName,
		Country:        countryCode,
	}

	log.Printf("Fetched weather data: %+v", weatherData)
	return weatherData, nil
}

// Fetch forecast data from FastAPI server
func fetchForecastData() ([]ForecastData, error) {
	log.Println("Fetching forecast from forecaster-service...")
	resp, err := http.Get("http://forecaster-service:5000/forecast")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var forecast []ForecastData
	if err := json.NewDecoder(resp.Body).Decode(&forecast); err != nil {
		return nil, err
	}

	log.Printf("Received %d forecast items", len(forecast))
	if len(forecast) > 0 {
		log.Printf("First item: %+v", forecast[0])
		log.Printf("Last item: %+v", forecast[len(forecast)-1])
	}

	return forecast, nil
}

// Update fetchForecastData to fetch the latest forecast item directly
func fetchLatestForecast() (*ForecastData, error) {
	log.Println("Fetching latest forecast from forecaster-service...")
	resp, err := http.Get("http://forecaster-service:5000/forecast/latest")
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Printf("Error fetching latest forecast or endpoint returned %d. Falling back to all forecasts.", resp.StatusCode)
		forecasts, err := fetchForecastData()
		if err != nil || len(forecasts) == 0 {
			return nil, fmt.Errorf("failed to fetch latest forecast: %v", err)
		}
		return &forecasts[len(forecasts)-1], nil
	}
	defer resp.Body.Close()

	var latestForecast ForecastData
	if err := json.NewDecoder(resp.Body).Decode(&latestForecast); err != nil {
		return nil, err
	}

	log.Printf("Received latest forecast: %+v", latestForecast)
	return &latestForecast, nil
}

// Broadcast forecast updates to WebSocket clients
func broadcastForecastUpdates() {
	for {
		forecast, err := fetchForecastData()
		if err != nil {
			log.Printf("Error fetching forecast data: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}

		forecastJSON, err := json.Marshal(forecast)
		if err != nil {
			log.Printf("Error marshaling forecast data: %v", err)
			continue
		}

		for client := range clients {
			err := client.WriteMessage(websocket.TextMessage, forecastJSON)
			if err != nil {
				log.Printf("Error sending forecast to client: %v", err)
				client.Close()
				delete(clients, client)
			}
		}

		time.Sleep(10 * time.Second)
	}
}

// WebSocket endpoint for forecast updates
func forecastWebSocket(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Error upgrading WebSocket connection: %v", err)
		return
	}
	defer conn.Close()

	clients[conn] = true
	log.Println("New WebSocket client connected")

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Error reading WebSocket message: %v", err)
			delete(clients, conn)
			break
		}
	}
}

// RunUI starts the Gin server for the UI
func RunUI() {
	// Initialize environment variables
	initEnv()

	r := gin.Default()

	staticPath, err := filepath.Abs("./static/")
	if err != nil {
		log.Fatalf("Failed to resolve static directory: %v", err)
	}
	r.Static("/static/", staticPath)

	templatePath, err := filepath.Abs("./templates/*")
	if err != nil {
		log.Fatalf("Failed to resolve templates directory: %v", err)
	}
	r.LoadHTMLGlob(templatePath)

	// ✅ Handle root endpoint to fetch forecast data
	// and pass the latest forecast to the template
	r.GET("/", func(c *gin.Context) {
		latestForecast, err := fetchLatestForecast()
		if err != nil {
			log.Printf("Forecast fetch error: %v", err)
			c.HTML(http.StatusOK, "index.html", gin.H{
				"error": "Could not load forecast",
			})
			return
		}

		log.Printf("DEBUG - Passing latest forecast: %+v", latestForecast) // Verify data
		c.HTML(http.StatusOK, "index.html", gin.H{
			"forecast": latestForecast, // Pass the single forecast struct
		})
	})

	// ✅ Handle form to fetch current weather only
	r.POST("/fetch", func(c *gin.Context) {
		city := c.PostForm("city")
		country := c.PostForm("country")

		weatherData, err := fetchWeatherData(city, country)
		if err != nil {
			c.HTML(http.StatusInternalServerError, "index.html", gin.H{
				"error": err.Error(),
			})
			return
		}

		latestForecast, err := fetchLatestForecast()
		if err != nil {
			log.Printf("Forecast fetch error: %v", err)
			c.HTML(http.StatusOK, "index.html", gin.H{
				"weather": weatherData,
				"error":   "Could not load forecast",
			})
			return
		}

		c.HTML(http.StatusOK, "index.html", gin.H{
			"weather":  weatherData,
			"forecast": latestForecast, // Pass the forecast data
		})
	})

	r.GET("/ws/forecast", forecastWebSocket)

	go broadcastForecastUpdates()

	r.Run(":8000")
}
