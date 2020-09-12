#!/bin/bash

sudo systemctl daemon-reload
sudo systemctl restart mysql
sudo systemctl restart nginx
sudo systemctl restart isuumo.go