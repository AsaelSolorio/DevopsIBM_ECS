from fastapi import FastAPI
import pandas as pd
from sklearn.linear_model import LinearRegression
import uvicorn
import os
import threading
import time
from psycopg import connect  # ✅ use plain psycopg!
from dotenv import load_dotenv
from datetime import datetime, timedelta

app = FastAPI()

load_dotenv()

latest_forecast = None

def fetch_weather_data_from_db():
    try:
        with connect(
            host=os.environ.get('DB_HOST'),
            dbname=os.environ.get('DB_NAME'),
            user=os.environ.get('DB_USERNAME'),
            password=os.environ.get('DB_PASSWORD')
        ) as conn:
            print("✅ Connected to database successfully")  # Debug print here
            cursor = conn.cursor()
            query = """
                SELECT temperature_max, created_at
                FROM weather_data
                ORDER BY created_at DESC LIMIT 150;
            """
            cursor.execute(query)
            data = cursor.fetchall()
            # Convert decimal.Decimal to float here
            data = [(float(row[0]), row[1]) for row in data]
            return data
    except Exception as e:
        print(f"Error fetching data from database: {e}")
        return []

def create_lag_features(df, lags=5):
    for i in range(1, lags + 1):
        df[f'lag_{i}'] = df['y'].shift(i)
    df = df.dropna()
    return df

def update_forecast():
    global latest_forecast

    while True:
        try:
            data = fetch_weather_data_from_db()
            if not data:
                print("No data fetched from database")
                time.sleep(10)
                continue

            df = pd.DataFrame(data, columns=['temperature_max', 'created_at'])
            df['ds'] = pd.to_datetime(df['created_at'])
            df['y'] = df['temperature_max']
            df = df[['ds', 'y']].sort_values('ds').reset_index(drop=True)

            print(f"Fetched {len(df)} rows. Temperature unique values: {df['y'].nunique()}")

            lags = 5

            # Fallback if data is constant or too small
            if df['y'].nunique() <= 1 or len(df) <= lags:
                print("Data constant or too small. Using mean as forecast.")
                mean_value = df['y'].mean()
                forecast = []
                for i in range(60):
                    next_time = df['ds'].iloc[-1] + timedelta(seconds=i + 1)
                    forecast.append({
                        'date': next_time.isoformat(),
                        'forecast_temp': float(mean_value),  # Convert here
                        'forecast_temp_lower': float(mean_value - 0.5),
                        'forecast_temp_upper': float(mean_value + 0.5),
                    })
                latest_forecast = forecast
                print(f"Forecast fallback with mean: {mean_value}")
                time.sleep(30)
                continue

            df_lags = create_lag_features(df.copy(), lags=lags)
            print(f"Lagged DataFrame shape: {df_lags.shape}")

            if df_lags.empty:
                print("Lagged dataframe is empty. Skipping forecast update.")
                time.sleep(30)
                continue

            X = df_lags[[f'lag_{i}' for i in range(1, lags + 1)]]
            y = df_lags['y']

            model = LinearRegression()
            model.fit(X, y)

            last_known = df['y'].iloc[-lags:].tolist()
            forecast = []
            last_date = df['ds'].iloc[-1]

            for i in range(60):
                x_input = last_known[-lags:]
                x_input_df = pd.DataFrame([x_input], columns=[f'lag_{j}' for j in range(1, lags + 1)])
                y_pred = model.predict(x_input_df)[0]
                last_known.append(y_pred)
                next_time = last_date + timedelta(seconds=i + 1)
                forecast.append({
                    'date': next_time.isoformat(),
                    'forecast_temp': float(y_pred),  # Convert here
                    'forecast_temp_lower': float(y_pred - 0.5),
                    'forecast_temp_upper': float(y_pred + 0.5),
                })

            latest_forecast = forecast
            print(f"Forecast updated: last point: {forecast[-1]}")

        except Exception as e:
            print(f"Error updating forecast: {e}")

        time.sleep(30)

def start_forecast_updater():
    thread = threading.Thread(target=update_forecast, daemon=True)
    thread.start()

start_forecast_updater()

@app.get("/forecast")
async def get_forecast():
    if latest_forecast is None:
        return []
    return latest_forecast

@app.get("/forecast/latest")
async def get_latest_forecast():
    if latest_forecast is None or len(latest_forecast) == 0:
        return {"error": "No forecast available"}
    return latest_forecast[-1]  # Return the most recent forecast

@app.get("/health_forecast")
async def health_check():
    return {"status": "ok from forecast"}

if __name__ == "__main__":
    uvicorn.run("mlForecast:app", host="0.0.0.0", port=5000, reload=True)
