#!/bin/sh

# Detect the Operating System
case "$(uname -s)" in

   Darwin)
     CENO_OS="Mac"
     ;;

   Linux)
     CENO_OS="Linux"
     ;;

   CYGWIN*|MINGW32*|MSYS*)
     echo "Windows is not supported by this script"
     echo "Please download the CENO Windows installer"
     exit 1
     ;;

   *)
     echo "Could not automatically detect your Operating System"
     echo "Manually download the latest release for your system from https://github.com/equalitie/ceno/releases/latest"
     exit 2
     ;;
esac

# Learn the latest release
LATEST_RELEASE=$(curl -s https://api.github.com/repos/equalitie/ceno/releases/latest | grep 'tag_' | cut -d\" -f4)

# Download and Unzip the latest release of CENOBox for this OS
echo "Downloading CENOBox Release" $LATEST_RELEASE "for" $CENO_OS
echo

curl -0 -J -L "https://github.com/equalitie/ceno/releases/download/$(echo $LATEST_RELEASE)/CENOBox_$(echo $CENO_OS).zip" -o "CENOBox_$(echo $CENO_OS).zip"
unzip -q CENOBox_$(echo $CENO_OS).zip

# Start CENOBox
echo
cd CENOBox

if [ "$CENO_OS" == "Linux" ]; then
  head -7 CENO.desktop > CENO.desktop
  echo Exec=sh `pwd`/CENO.sh start >> CENO.desktop
  echo Path=`pwd` >> CENO.desktop
  echo Icon=`pwd`/icon.png
  cp CENO.desktop ~/local/share/applications/CENO.desktop
fi

sh ./CENO.sh start
cd ..
