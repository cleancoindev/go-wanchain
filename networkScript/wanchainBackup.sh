#!/bin/sh
# back up go-wanchain geth data
# hot backup will use admin.exportChain to backup
# cold backup will kill geth first, tar, then restart


#   __        ___    _   _  ____ _           _       ____             

#   \ \      / / \  | \ | |/ ___| |__   __ _(_)_ __ |  _ \  _____   __

#    \ \ /\ / / _ \ |  \| | |   | '_ \ / _` | | '_ \| | | |/ _ \ \ / /

#     \ V  V / ___ \| |\  | |___| | | | (_| | | | | | |_| |  __/\ V / 

#      \_/\_/_/   \_\_| \_|\____|_| |_|\__,_|_|_| |_|____/ \___| \_/  

# 

version="v0.9.4"
WANPATH=$HOME/wanchain/$version
wanchainLogPath=$HOME/wanchainbackup/logbackup/wanchainlog.txt
HOTBACKUPDIR=$HOME/wanchainbackup/hotbackup
COLDBACKUPDIR=$HOME/wanchainbackup/coldbackup
restartflag=0
backupNum=56

ipcPath=$HOME/.wanchain/gwan.ipc
GETHDIR=$HOME/.wanchain/geth

hotlog=$HOTBACKUPDIR/log.txt
coldlog=$COLDBACKUPDIR/log.txt

if [ ! -d $HOTBACKUPDIR ] ; then
    mkdir -p "$HOTBACKUPDIR"
    echo "HOTBACKUPDIR not exist: "$HOTBACKUPDIR", create it!" >> $hotlog
fi

cd $WANPATH
echo "************************************************" >> $hotlog
echo "****** go-wanchain data hot Backup begin ******" >> $hotlog
echo "************************************************" >> $hotlog

DATE=`date '+%Y%m%d-%H%M%S'`
backupChainName=$DATE"-wanchain"

echo " *** BACKUPTIME: " $DATE >> $hotlog

echo " " >> $hotlog
echo "admin.exportChain('$HOTBACKUPDIR/$backupChainName')" | ./bin/geth attach ipc:$ipcPath exit 2>&1 >> $hotlog
echo " *** BACKUP Chain Name: " $backupChainName >> $hotlog

if [ $(ls $HOTBACKUPDIR -l | grep "wanchain" | wc -l) -gt $backupNum ]
then
    echo "the backup number in folder" $HOTBACKUPDIR " is larger than " $backupNum ", follow files would be rm" >> $hotlog
    ls $HOTBACKUPDIR -rt | head -n1 >> $hotlog
    cd $HOTBACKUPDIR
    rm -r $(ls $HOTBACKUPDIR -rt | head -n1) >> $hotlog
fi

echo "****** go-wanchain hot Backup end******" >> $hotlog
echo " " >> $hotlog
echo " " >> $hotlog
echo " " >> $hotlog

if [ ! -d $COLDBACKUPDIR ] ; then
    mkdir -p "$COLDBACKUPDIR"
    echo "COLDBACKUPDIR not exist: "$COLDBACKUPDIR", create it!" >> $coldlog
fi

cd $WANPATH
echo "************************************************" >> $coldlog
echo "****** go-wanchaia data cold Backup begin ******" >> $coldlog
echo "************************************************" >> $coldlog

DATE=`date '+%Y%m%d-%H%M%S'`
backupGethName=$DATE"-geth.tar"
echo " *** BACKUPTIME:" $DATE >> $coldlog
	
cd $GETHDIR
echo " " >> $coldlog

if [ $(ps -ef | grep geth | grep -v 'grep\|attach\|daemon_geth' | wc -l) -gt 0 ]
then
    echo "before compress wan-chain geth data, the geth process will be killed" >> $coldlog
    proinfo=`ps -ef | grep geth | grep -v 'grep\|attach\|daemon_geth'`
    label=`echo $proinfo | awk '{print $7}'`
    CMDinfo=`echo ${proinfo#*$label}`
    echo "follow process will be killed" >> $coldlog
    echo $CMDinfo >> $coldlog
    ps -ef | grep geth | grep -v 'grep\|attach\|daemon_geth' | awk '{print $2}' | xargs kill -9
    restartflag=1
fi

echo "begin to tar geth file under " $GETHDIR >> $coldlog
echo " *** filelist *** " >> $coldlog
tar czvf $COLDBACKUPDIR/$backupGethName * >> $coldlog 2>&1

if [ $? -eq 0 ]; then
    echo " *** BACKUP Geth data Name:" $backupGethName " Successsful">> $coldlog
else
    echo " *** BACKUP Geth data Name:" $backupGethName " Fail!">> $coldlog
fi

if [ $(ls $COLDBACKUPDIR -l | grep "geth.tar" | wc -l) -gt $backupNum ]
then
    echo "the backup number in folder" $COLDBACKUPDIR " is larger than " $backupNum ", follow files would be rm" >> $coldlog
    ls $COLDBACKUPDIR -rt | head -n1 >> $coldlog
    cd $COLDBACKUPDIR
    rm -r $(ls $COLDBACKUPDIR -rt | head -n1) >> $coldlog
fi

echo "****** go-wanchain cold Backup end******" >> $coldlog
echo " " >> $coldlog
echo " " >> $coldlog
echo " " >> $coldlog

if [ $restartflag -eq 1 ]
then
    #This will recall the geth command
    cd $WANPATH  
    $CMDinfo >> $wanchainLogPath 2>&1
fi