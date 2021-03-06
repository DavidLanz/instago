package igdl

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/siongui/instago"
)

func isTrayInQueue(queue []instago.IGReelTray, tray instago.IGReelTray) bool {
	for _, t := range queue {
		if t.Id == tray.Id {
			return true
		}
	}
	return false
}

func (m *IGDownloadManager) GetStoryItemAndReelMentions(item instago.IGItem, username string) (err error) {
	isDownloaded, err := getStoryItem(item, username)
	if err != nil {
		return
	}

	if item.Audience == "besties" {
		PrintBestiesItemInfo(item, username)
	}

	fetched := make(map[string]bool)
	if isDownloaded {
		for _, rm := range item.ReelMentions {
			PrintReelMentionInfo(rm)
			if rm.GetUsername() == username {
				fmt.Println("reel mention self. download ignored.")
				continue
			}

			if rm.IsPublic() {
				if _, ok := fetched[rm.GetUsername()]; !ok {
					m.SmartDownloadStory(rm)
					fetched[rm.GetUsername()] = true
				}
				// handle err of m.downloadUserStoryPostlive(rm.GetUserId()) ?
				/*
					err = m.downloadUserStoryPostlive(rm.GetUserId())
					if err != nil {
						PrintUsernameIdMsg(rm.GetUsername(), rm.GetUserId(), err)
						return
					}
				*/
			}
		}
	}
	return
}

func (m *IGDownloadManager) DownloadZeroItemUsers(c chan instago.IGReelTray, interval int, verbose bool) {
	queue := []instago.IGReelTray{}
	for {
		select {
		case tray := <-c:
			// append to queue if not exist
			id := tray.Id
			username := tray.GetUsername()
			if verbose {
				UsernameIdColorPrint(username, id)
				fmt.Println("legnth of channel:", len(c))
			}
			if isTrayInQueue(queue, tray) {
				if verbose {
					PrintUsernameIdMsg(username, id, "exist. ignore.")
				}
			} else {
				queue = append(queue, tray)
				if verbose {
					PrintUsernameIdMsg(username, id, "appended")
				}
			}
		default:
			if len(queue) > 0 {
				tray := queue[0]
				queue = queue[1:]

				id := strconv.FormatInt(tray.Id, 10)
				username := tray.GetUsername()
				if verbose {
					PrintUsernameIdMsg(username, id, " downloading...")
				}

				go func() {
					// FIXME: take besties into account
					ut, err := m.SmartGetUserStory(tray.User)
					if err != nil {
						PrintUsernameIdMsg(username, id, err)
						queue = append(queue, tray)
						return
					}

					for _, item := range ut.Reel.GetItems() {
						err = m.GetStoryItemAndReelMentions(item, ut.Reel.GetUsername())
						if err != nil {
							PrintUsernameIdMsg(username, id, err)
							queue = append(queue, tray)
							return
						}
					}

					err = DownloadPostLiveItem(ut.PostLiveItem)
					if err == nil {
						if verbose {
							PrintUsernameIdMsg(username, id, " Download Success.")
						}
					} else {
						PrintUsernameIdMsg(username, id, err)
						queue = append(queue, tray)
					}
				}()
			}

			if verbose {
				fmt.Println("current queue length: ", len(queue))
				PrintMsgSleep(interval, "DownloadZeroItemUsers: ")
			} else {
				SleepSecond(interval)
			}
		}
	}
}

func isLatestReelMediaDownloaded(username string, latestReelMedia int64) bool {
	utimes, err := GetReelMediaUnixTimesInUserStoryDir(username)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Println("In isLatestReelMediaDownloaded", err)
		}
		return false
	}

	lrm := strconv.FormatInt(latestReelMedia, 10)
	for _, utime := range utimes {
		if lrm == utime {
			return true
		}
	}
	return false
}

// Use (25, 2, false) as arguments is good. will not cause http 429
func (m *IGDownloadManager) DownloadStoryAndPostLiveForever(interval1, interval2 int, verbose bool) {
	// channel for waiting DownloadPostLive completed
	isDownloading := make(map[string]bool)

	// usually there are at most 150 trays in reels_tray.
	// double the buffer to 300. 160 or 200 may be ok as well.
	c := make(chan instago.IGReelTray, 300)

	go m.DownloadZeroItemUsers(c, interval2, verbose)
	for {
		rt, err := m.GetReelsTray()
		if err != nil {
			log.Println(err)
			continue
		}

		// TODO: use channel for postlive?
		go DownloadPostLive(rt.PostLive, isDownloading)
		go PrintLiveBroadcasts(rt.Broadcasts)

		//for index, tray := range rt.Trays {
		for _, tray := range rt.Trays {
			username := tray.GetUsername()
			id := tray.Id
			//items := tray.GetItems()

			if isLatestReelMediaDownloaded(username, tray.LatestReelMedia) {
				if verbose {
					PrintUsernameIdMsg(username, id, " all downloaded")
				}
				continue
			}

			if tray.HasBestiesMedia {
				PrintUsernameIdMsg(username, id, "has close friend (besties) story item(s)")
			}

			if verbose {
				UsernameIdColorPrint(username, id)
				fmt.Println(" has undownloaded items")
			}
			c <- tray
			/*
				if len(items) == 0 {
					if verbose {
						UsernameIdColorPrint(username, id)
						fmt.Println("#", index, " send to channel")
					}
					c <- tray
				} else {
					for _, item := range items {
						err = m.GetStoryItemAndReelMentions(item, username)
						if err != nil {
							PrintUsernameIdMsg(username, id, err)
							c <- tray
							break
						}
					}
					// is there postlive items in tray here?
				}
			*/
		}

		PrintMsgSleep(interval1, "DownloadStoryAndPostLiveForever: ")
	}
}

func (m *IGDownloadManager) DownloadUnexpiredStoryOfUser(user instago.User) (err error) {
	// In case user change privacy, read user info via mobile api endpoint
	// first.
	u, err := m.SmartGetUserInfoEndPoint(user.GetUserId())
	if err == nil {
		err = m.SmartDownloadUserStoryPostliveLayer(u, 2)
	} else {
		log.Println(err)
		log.Println("Fail to fetch user info via mobile endpoint. use old user info data to fetch")
		err = m.SmartDownloadUserStoryPostliveLayer(user, 2)
	}
	return
}

func (m *IGDownloadManager) DownloadUnexpiredStoryOfFollowUsers(users []instago.IGFollowUser, interval int) (err error) {
	for _, user := range users {
		err = m.DownloadUnexpiredStoryOfUser(user)
		if err != nil {
			log.Println(err)
		}
		SleepLog(interval)
	}
	return
}

// sleep "interval" seconds after fetching each user
func (m *IGDownloadManager) DownloadUnexpiredStoryOfAllFollowingUsers(interval int) (err error) {
	log.Println("Load following users from data dir first")
	users, err := LoadLatestFollowingUsers()
	if err != nil {
		log.Println(err)
		log.Println("Fail to load users from data dir. Try to load following users from Instagram")
		users, err = m.GetSelfFollowing()
		if err != nil {
			log.Println(err)
			return
		}
	}

	return m.DownloadUnexpiredStoryOfFollowUsers(users, interval)
}
