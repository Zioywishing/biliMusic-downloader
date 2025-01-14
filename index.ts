import axios from "axios";
// @ts-ignore
import fs from "fs";
// @ts-ignore
import path from "path";
// @ts-ignore
import { exec } from "child_process";
import cookie from "./cookie.ts";

const headers = {
	"accept": "application/json, text/plain, */*",
	"accept-language": "zh-CN,zh;q=0.9,en;q=0.8,en-GB;q=0.7,en-US;q=0.6",
	"cache-control": "no-cache",
	cookie,
	"dnt": "1",
	"origin": "https://www.bilibili.com",
	"pragma": "no-cache",
	"priority": "u=1, i",
	"referer": "https://www.bilibili.com/video/BV1wg4y127mJ/?spm_id_from=333.337.search-card.all.click&vd_source=f4b11eff4d5b11ae41cb4e0ca94e674b",
	"sec-ch-ua": '"Chromium";v="128", "Not;A=Brand";v="24", "Microsoft Edge";v="128"',
	"sec-ch-ua-mobile": "?0",
	"sec-ch-ua-platform": '"Windows"',
	"sec-fetch-dest": "empty",
	"sec-fetch-mode": "cors",
	"sec-fetch-site": "same-site",
	"user-agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36 Edg/128.0.0.0"
};

const sanitizeFileName = (name: string) => {
	return name.replace(/[<>:"/\\|?*]+/g, "_");
};

const getVideoInfo = async (bvid: string): Promise<{ cid: any; partName: any }[]> => {
	const res = await axios.get(`https://api.bilibili.com/x/player/pagelist?bvid=${bvid}`);
	return res.data.data.map((v: { cid: any; part: any }) => ({
		cid: v.cid,
		partName: sanitizeFileName(v.part)
	}));
};

const getAudioUrl = async (bvid: string, cid: string) => {
	const url = "https://api.bilibili.com/x/player/wbi/playurl";

	const params = {
		fnval: 4048,
		bvid,
		cid
	};

	const response = await axios.get(url, { params, headers });
	// console.log(response.data);
	return response.data.data.dash.audio[0].baseUrl;
};

const getFileStream = async url => {
	const response = await axios({
		method: "get",
		url: url,
		responseType: "stream",
		headers
	});
	return response.data;
};

const convertToMp3Stream = (inputStream, outputPath) => {
	// Ensure the directory exists
	const dir = path.dirname(outputPath);
	if (!fs.existsSync(dir)) {
		fs.mkdirSync(dir, { recursive: true });
	}
	return new Promise((resolve, reject) => {
		const ffmpegProcess = exec(`ffmpeg -i pipe:0 -q:a 0 "${outputPath}"`, (error, stdout, stderr) => {
			if (error) {
				reject(`Error: ${error.message}`);
				return;
			}
			if (stderr) {
				console.error(`stderr: ${stderr}`);
			}
			resolve(stdout);
		});
		inputStream.pipe(ffmpegProcess.stdin);
	});
};

(async () => {
	const bvid = "BV1DR4y1R7ni";
	const infoList = await getVideoInfo(bvid);
	for (let info of infoList) {
		(async () => {
			const { cid, partName } = info;
			const audioUrl = await getAudioUrl(bvid, cid);
			const audioStream = await getFileStream(audioUrl);
			const mp3Path = path.resolve(`./download/${bvid}`, `${partName}.mp3`);
			await convertToMp3Stream(audioStream, mp3Path);
			console.log(`下载${partName}完成`);
		})();
	}
})();
