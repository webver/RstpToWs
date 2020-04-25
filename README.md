# RstpToWs
https://github.com/deepch/RTSPtoWSMP4f fork
## Configuration

### Edit file config.json

format:

```bash
{
  "server": {
    "http_port": ":8083"
  },
  "streams": {
    "testCam0": {
      "url": "rtsp://admin:admin@172.20.12.52:554/h264"
    },
    "testCam1": {
      "url": "rtsp://170.93.143.139/rtplive/470011e600ef003a004ee33696235daa"
    },
    "testCam2": {
      "url": "rtsp://170.93.143.139/rtplive/470011e600ef003a004ee33696235daa"
    }
  }
}
```
## Status
GET /status

```bash
{
   "server":{
      "http_port":":8083"
   },
   "streams":{
      "testCam0":{ //suuid камеры
         "url":"rtsp://admin:admin@172.20.12.52:554/h264",
         "status":true, //Есть коннект к камере или нет
         "Codecs":[
            {
               "Record":"AU0AH//hADBnTQAfmmQCgC3/gLcBAQFAAAD6AAAw1DoYACFTAACFSa7y40MABCpgABCpNd5cKAABAARo7jyA",
               "RecordInfo":{
                  "AVCProfileIndication":77,
                  "ProfileCompatibility":0,
                  "AVCLevelIndication":31,
                  "LengthSizeMinusOne":3,
                  "SPS":[
                     "Z00AH5pkAoAt/4C3AQEBQAAA+gAAMNQ6GAAhUwAAhUmu8uNDAAQqYAAQqTXeXCgA"
                  ],
                  "PPS":[
                     "aO48gA=="
                  ]
               },
               "SPSInfo":{
                  "ProfileIdc":77,
                  "LevelIdc":31,
                  "MbWidth":80,
                  "MbHeight":45,
                  "CropLeft":0,
                  "CropRight":0,
                  "CropTop":0,
                  "CropBottom":0,
                  "Width":1280,
                  "Height":720
               }
            }
         ],
         "Clients":{ //Кто смотрит камеру
            "14DB9D02-4D01-D8AA-9EED-C6BA848A7BBC":{

            }
         }
      }
   }
}
```

## Limitations

Video Codecs Supported: H264 all profiles

Audio Codecs Supported: AAC mono

## Test

CPU usage 0.2% one core cpu intel core i7 / stream
