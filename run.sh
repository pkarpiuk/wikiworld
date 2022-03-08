export DATA_DIR=/home/ubuntu/wiki-data
docker run -d --restart=unless-stopped --name=daily-wikidumptools -v ${DATA_DIR}:/db -e WIKIPEDIA_LANGUAGES='pl en be ru uk lt lv et de' wikidumptools daily
docker run -d --restart=unless-stopped --name=hourly-wikidumptools -v ${DATA_DIR}:/db -e WIKIPEDIA_LANGUAGES='pl en be ru uk lt lv et de' wikidumptools hourly
docker run -d --restart=unless-stopped --name=rest-wikidumptools -v ${DATA_DIR}:/db -e WIKIPEDIA_LANGUAGES='pl en be ru uk lt lv et de' wikidumptools rest
docker run -d --restart=unless-stopped --name=events-wikidumptools -v ${DATA_DIR}:/db -e EVENTS_DAYS=64 wikidumptools events
