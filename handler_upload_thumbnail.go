package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}


	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	const maxMemory = 10 << 20
	r.ParseMultipartForm(maxMemory)

	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error parsing thumbnail", err)
		return
	}
	defer file.Close()

	media_type := header.Header.Get("Content-Type")
	if media_type == ""{
		respondWithError(w, http.StatusBadRequest, "No content type", nil)
		return
	}

	// data, err := io.ReadAll(file)
	// if err != nil {
	// 	respondWithError(w, http.StatusInternalServerError, "error parsing thumbnail", err)
	// 	return
	// }

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error getting video metadata", err)
		return
	}

	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Unauthorised", err)
		return
	}

	mediaType, _, err := mime.ParseMediaType(media_type)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error parsing media type", err)
		return
	}

	if mediaType != "image/jpeg" && mediaType != "image/png" {
		respondWithError(w, http.StatusBadRequest, "Wrong media type", nil)
		return
	}

	ext := ""
	if mediaType == "image/png" {
		ext = ".png"
	} else if mediaType == "image/jpeg" {
		ext = ".jpg"
	} else {
		respondWithError(w, http.StatusInternalServerError, "cound not determine file extension", err)
		return
	}

	memorySpace := make([]byte, 32)
	_, err = rand.Read(memorySpace)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error making rand byte slice", err)
	}
	randString := base64.RawURLEncoding.EncodeToString(memorySpace)


	filename := randString + ext
	assetPath := filepath.Join(cfg.assetsRoot, filename)
	
	assetFile, err := os.Create(assetPath)
	if err!= nil {
		respondWithError(w, http.StatusInternalServerError, "error creating thumbnail file", err)
		return
	}
	defer assetFile.Close()
	if _, err := io.Copy(assetFile, file); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error copying file from memory", err)
	}


	url := fmt.Sprintf("http://localhost:%s/assets/%s", cfg.port, filename)
	// stringEncodedThumbnailData := base64.StdEncoding.EncodeToString(data)
	// dataURL := fmt.Sprintf("data:%s;base64,%s", media_type, stringEncodedThumbnailData)

	// url := fmt.Sprintf("http://localhost:8091/api/thumbnails/%s", video.ID)

	video.ThumbnailURL = &url

	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error updating video metadata", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}
