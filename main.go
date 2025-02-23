package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"sync"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

var (
	version = "dev"
	date    = ""
)

func main() {
	showVersion := flag.Bool("version", false, "Show version")
	flag.BoolVar(showVersion, "v", false, "Show version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("Impartus Video Downloader\nVersion: %s\nBuild Date: %s\n", version, date)
		os.Exit(0)
	}

	_, err := exec.LookPath("ffmpeg")
	if err != nil {
		log.Fatalln("Please add ffmpeg to your path")
	}

	// Logging
	logFile, err := os.OpenFile("run.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Println("Could not start logs")
	}
	defer logFile.Close()

	log.SetOutput(logFile)

	fmt.Println("Impartus Video Downloader")
	fmt.Println("If you are facing any issues, please check the section at https://github.com/pnicto/impartus-video-downloader#faqtroubleshooting")
	fmt.Print("\n\n")

	LoginAndSetToken()
	courses := GetCourses()

	courseIndex := ChooseCourse(courses)
	lectures := GetLectures(courses[courseIndex])

	startLectureIndex, endLectureIndex, skipEmptyLectures := ChooseLectures(lectures)
	var chosenLectures Lectures
	if skipEmptyLectures {
		chosenLectures = removeEmptyLectures(lectures[startLectureIndex : endLectureIndex+1])
	} else {
		chosenLectures = lectures[startLectureIndex : endLectureIndex+1]
	}

	config := GetConfig()
	if config.Slides {
		for _, lecture := range chosenLectures {
			DownloadLectureSlides(lecture)
		}
	}

	playlists := GetPlaylist(chosenLectures)

	err = os.MkdirAll(config.TempDirLocation, 0755)
	if err != nil {
		log.Fatalln("Could not create temp dir")
	}
	fmt.Println()

	numWorkers := config.NumWorkers
	playlistJobs := make(chan ParsedPlaylist, numWorkers)

	p := mpb.New(mpb.WithWidth(70))

	downloadBar := p.AddBar(int64(len(playlists)),
		mpb.PrependDecorators(
			decor.Name("Downloaded ", decor.WCSyncWidth),
			decor.CountersNoUnit("%d / %d", decor.WCSyncWidth),
		),
		mpb.AppendDecorators(decor.Percentage(decor.WCSyncWidth)),
		mpb.BarPriority(math.MaxInt-1),
	)

	joiningBar := p.AddBar(int64(len(playlists)),
		mpb.PrependDecorators(
			decor.Name("Joined ", decor.WCSyncWidth),
			decor.CountersNoUnit("%d / %d", decor.WCSyncWidth),
		),
		mpb.AppendDecorators(decor.Percentage(decor.WCSyncWidth)),
		mpb.BarPriority(math.MaxInt),
	)

	var joinWg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		go func() {
			for playlist := range playlistJobs {
				// fmt.Println("Downloading playlist: ", playlist.Title, playlist.SeqNo)
				downloadedPlaylist := DownloadPlaylist(playlist, p)
				metadataFile := CreateTempM3U8File(downloadedPlaylist)
				downloadBar.Increment()
				// fmt.Println("Downloaded playlist: ", playlist.Title, playlist.SeqNo)

				go func(file M3U8File) {
					defer joiningBar.Increment()
					defer joinWg.Done()
					// fmt.Println("Joining chunks for: ", file.Playlist.Title, file.Playlist.SeqNo)
					var left, right string
					if file.FirstViewFile != "" && config.Views != "right" {
						left = JoinChunksFromM3U8(file.FirstViewFile, fmt.Sprintf("LEC %03d %s LEFT VIEW.mp4", file.Playlist.SeqNo, file.Playlist.Title))
					}
					if file.SecondViewFile != "" && config.Views != "left" {
						right = JoinChunksFromM3U8(file.SecondViewFile, fmt.Sprintf("LEC %03d %s RIGHT VIEW.mp4", file.Playlist.SeqNo, file.Playlist.Title))
					}

					if left != "" && right != "" && config.Views == "both" {
						JoinViews(left, right, fmt.Sprintf("LEC %03d %s", file.Playlist.SeqNo, file.Playlist.Title))
					}
					// fmt.Println("Joined chunks for: ", file.Playlist.Title, file.Playlist.SeqNo)
				}(metadataFile)
			}
		}()
	}

	for _, playlist := range playlists {
		// fmt.Println("Adding playlist to job queue: ", playlist.Title, playlist.SeqNo)
		joinWg.Add(1)
		playlistJobs <- playlist
	}

	joinWg.Wait()
	p.Wait()
	close(playlistJobs)

	fmt.Print("\n\n")
	fmt.Println("Please delete the temp directory if you are running low on space. The temp directory is located at:", config.TempDirLocation)
	fmt.Println("If you are facing any issues, please check the section at https://github.com/pnicto/impartus-video-downloader#faqtroubleshooting")
	fmt.Print("\n\n")
	fmt.Println("It is recommended that you use this tool as sparingly as possible. Heavy usage of this tool puts more strain on impartus server leading to potential IP bans, breaking API changes and possibly legal action.")
	fmt.Println("If this project helped you, consider starring it on GitHub: https://github.com/pnicto/impartus-video-downloader")
}
