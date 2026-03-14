package supabase

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/coding-tutor/internal/config"
)

func Get(path string, result interface{}) error {
	url := config.Global.Supabase.URL + "/rest/v1/" + path
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("apikey", config.Global.Supabase.ServiceRoleKey)
	req.Header.Set("Authorization", "Bearer "+config.Global.Supabase.ServiceRoleKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("supabase get error %d: %s", resp.StatusCode, string(body))
	}
	return json.Unmarshal(body, result)
}

func Insert(table string, data interface{}) error {
	body, err := json.Marshal(data)
	if err != nil {
		return err
	}

	url := config.Global.Supabase.URL + "/rest/v1/" + table
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("apikey", config.Global.Supabase.ServiceRoleKey)
	req.Header.Set("Authorization", "Bearer "+config.Global.Supabase.ServiceRoleKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Prefer", "return=minimal")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("supabase insert error %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func Patch(path string, data interface{}) error {
	body, err := json.Marshal(data)
	if err != nil {
		return err
	}

	url := config.Global.Supabase.URL + "/rest/v1/" + path
	req, err := http.NewRequest("PATCH", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("apikey", config.Global.Supabase.ServiceRoleKey)
	req.Header.Set("Authorization", "Bearer "+config.Global.Supabase.ServiceRoleKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Prefer", "return=minimal")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("supabase patch error %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func Delete(path string) error {
	url := config.Global.Supabase.URL + "/rest/v1/" + path
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("apikey", config.Global.Supabase.ServiceRoleKey)
	req.Header.Set("Authorization", "Bearer "+config.Global.Supabase.ServiceRoleKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("supabase delete error %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func Upsert(table string, data interface{}) error {
	body, err := json.Marshal(data)
	if err != nil {
		return err
	}

	url := config.Global.Supabase.URL + "/rest/v1/" + table
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("apikey", config.Global.Supabase.ServiceRoleKey)
	req.Header.Set("Authorization", "Bearer "+config.Global.Supabase.ServiceRoleKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Prefer", "resolution=merge-duplicates,return=minimal")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("supabase upsert error %d: %s", resp.StatusCode, string(b))
	}
	return nil
}
