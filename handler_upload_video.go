package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"os"
	"os/exec"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {

	const maxMemory = 1 << 30
	r.Body = http.MaxBytesReader(w, r.Body, maxMemory)

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

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error retrieving video metadata", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Only owners can upload video", err)
		return
	}

	r.ParseMultipartForm(maxMemory)
	
	file, fileHeader, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error getting data from request", err)
		return
	}
	defer file.Close()
	
	media_type := fileHeader.Header.Get("Content-Type")
	if media_type == ""{
		respondWithError(w, http.StatusBadRequest, "No content type", nil)
		return
	}

	mediaType, _, err := mime.ParseMediaType(media_type)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error parsing media type", err)
		return
	}
	ext := ""
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Wrong media type", nil)
		return
	}
	ext = ".mp4"

	fmt.Println("uploading video", videoID, "by user", userID)

	tempFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error creating temp file", err)
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()
	_, err = io.Copy(tempFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error copying to temp", err)
		return
	}
	// _, err = tempFile.Seek(0, io.SeekStart)
	// if err != nil {
	// 	respondWithError(w, http.StatusInternalServerError, "Error copying to temp", err)
	// 	return
	// }

	processedVideoPath, err := processVideoForFastStart(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error ffmpeging the moov atom", err)
		return
	}

	newTempFile, err := os.Open(processedVideoPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error opening newtemp file", err)
		return
	}
	defer os.Remove(newTempFile.Name())
	defer newTempFile.Close()

	aspectRatio, err := getVideoAspectRatio(newTempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error getting aspect ratio", err)
		return
	}
	
	memorySpace := make([]byte, 32)
	_, err = rand.Read(memorySpace)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error making rand byte slice", err)
	}
	randString := base64.RawURLEncoding.EncodeToString(memorySpace)


	filename := aspectRatio + "/" + randString + ext


	putObjectParams := s3.PutObjectInput{
		Bucket: &cfg.s3Bucket,
		Key: &filename,
		Body: newTempFile,
		ContentType: &mediaType,
	}
	_, err = cfg.s3Client.PutObject(r.Context(), &putObjectParams)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error putting object into bucket", err)
		return
	
	}
	// videoUrl := fmt.Sprintf("%s,%s",cfg.s3Bucket, filename)

	videoUrl := fmt.Sprintf("%s/%s",cfg.s3CfDistribution, filename)
	video.VideoURL = &videoUrl

	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error updating video metadata", err)
		return
	}

	// signedVideo, err := cfg.dbVideoToSignedVideo(video)
	// if err != nil {
	// 	respondWithError(w, http.StatusInternalServerError, "Error getting signed video", err)
	// 	return
	// }

	respondWithJSON(w, http.StatusOK, video)
}


func getVideoAspectRatio(filePath string) (string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	buff := bytes.Buffer{}
	cmd.Stdout = &buff
	cmd.Run()

	type VideoData struct {
	Streams []struct {
		Index              int    `json:"index"`
		CodecName          string `json:"codec_name,omitempty"`
		CodecLongName      string `json:"codec_long_name,omitempty"`
		Profile            string `json:"profile,omitempty"`
		CodecType          string `json:"codec_type"`
		CodecTagString     string `json:"codec_tag_string"`
		CodecTag           string `json:"codec_tag"`
		Width              int    `json:"width,omitempty"`
		Height             int    `json:"height,omitempty"`
		CodedWidth         int    `json:"coded_width,omitempty"`
		CodedHeight        int    `json:"coded_height,omitempty"`
		ClosedCaptions     int    `json:"closed_captions,omitempty"`
		FilmGrain          int    `json:"film_grain,omitempty"`
		HasBFrames         int    `json:"has_b_frames,omitempty"`
		SampleAspectRatio  string `json:"sample_aspect_ratio,omitempty"`
		DisplayAspectRatio string `json:"display_aspect_ratio,omitempty"`
		PixFmt             string `json:"pix_fmt,omitempty"`
		Level              int    `json:"level,omitempty"`
		ColorRange         string `json:"color_range,omitempty"`
		ColorSpace         string `json:"color_space,omitempty"`
		ColorTransfer      string `json:"color_transfer,omitempty"`
		ColorPrimaries     string `json:"color_primaries,omitempty"`
		ChromaLocation     string `json:"chroma_location,omitempty"`
		FieldOrder         string `json:"field_order,omitempty"`
		Refs               int    `json:"refs,omitempty"`
		IsAvc              string `json:"is_avc,omitempty"`
		NalLengthSize      string `json:"nal_length_size,omitempty"`
		ID                 string `json:"id"`
		RFrameRate         string `json:"r_frame_rate"`
		AvgFrameRate       string `json:"avg_frame_rate"`
		TimeBase           string `json:"time_base"`
		StartPts           int    `json:"start_pts"`
		StartTime          string `json:"start_time"`
		DurationTs         int    `json:"duration_ts"`
		Duration           string `json:"duration"`
		BitRate            string `json:"bit_rate,omitempty"`
		BitsPerRawSample   string `json:"bits_per_raw_sample,omitempty"`
		NbFrames           string `json:"nb_frames"`
		ExtradataSize      int    `json:"extradata_size"`
		Disposition        struct {
			Default         int `json:"default"`
			Dub             int `json:"dub"`
			Original        int `json:"original"`
			Comment         int `json:"comment"`
			Lyrics          int `json:"lyrics"`
			Karaoke         int `json:"karaoke"`
			Forced          int `json:"forced"`
			HearingImpaired int `json:"hearing_impaired"`
			VisualImpaired  int `json:"visual_impaired"`
			CleanEffects    int `json:"clean_effects"`
			AttachedPic     int `json:"attached_pic"`
			TimedThumbnails int `json:"timed_thumbnails"`
			NonDiegetic     int `json:"non_diegetic"`
			Captions        int `json:"captions"`
			Descriptions    int `json:"descriptions"`
			Metadata        int `json:"metadata"`
			Dependent       int `json:"dependent"`
			StillImage      int `json:"still_image"`
			Multilayer      int `json:"multilayer"`
		} `json:"disposition"`
		Tags struct {
			Language    string `json:"language"`
			HandlerName string `json:"handler_name"`
			VendorID    string `json:"vendor_id"`
			Encoder     string `json:"encoder"`
			Timecode    string `json:"timecode"`
		} `json:"tags,omitempty"`
		SampleFmt      string `json:"sample_fmt,omitempty"`
		SampleRate     string `json:"sample_rate,omitempty"`
		Channels       int    `json:"channels,omitempty"`
		ChannelLayout  string `json:"channel_layout,omitempty"`
		BitsPerSample  int    `json:"bits_per_sample,omitempty"`
		InitialPadding int    `json:"initial_padding,omitempty"`
	} `json:"streams"`
	}

	metaData := VideoData{}

	if err := json.Unmarshal(buff.Bytes(), &metaData); err != nil {
		return "", err
	}

	width := metaData.Streams[0].Width
	height := metaData.Streams[0].Height

	return determineAspectRatio(width, height), nil
}

func determineAspectRatio(width, height int) string {
	if width == 0 || height == 0 {
		return "other"
	}

	ratio := float64(width) / float64(height)
	const epsilon = 0.01 // small tolerance for rounding errors

	if math.Abs(ratio-16.0/9.0) < epsilon {
		return "landscape"
	} else if math.Abs(ratio-9.0/16.0) < epsilon {
		return "portrait"
	} else {
		return "other"
	}
}

func processVideoForFastStart(filePath string) (string, error) {
	newPath := filePath + ".processing"

	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", newPath)
	err := cmd.Run()
	if err != nil {
		return "", err
	}

	return newPath, nil
}

// func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {

// 	newPresignClient := s3.NewPresignClient(s3Client)
// 	preSignObjectParams := s3.GetObjectInput{
// 		Bucket: &bucket,
// 		Key: &key,
// 	}

// 	req, err := newPresignClient.PresignGetObject(context.Background(), &preSignObjectParams, s3.WithPresignExpires(expireTime))
// 	if err != nil {
// 		return "", err
// 	}

// 	return req.URL, nil
// }

// func(cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
// 	if video.VideoURL == nil {
// 		return video, nil
// 	}

// 	bucketnkey := strings.Split(*video.VideoURL, ",")
// 	if len(bucketnkey) != 2 {
// 		return database.Video{}, fmt.Errorf("error spliting video url for sign conversion")
// 	}

// 	preSignedUrl, err := generatePresignedURL(cfg.s3Client, bucketnkey[0], bucketnkey[1], time.Hour)
// 	if err != nil {
// 		return database.Video{}, err
// 	}

// 	video.VideoURL = &preSignedUrl
// 	return video, nil
// }