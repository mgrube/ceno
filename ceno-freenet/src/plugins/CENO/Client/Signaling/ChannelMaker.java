package plugins.CENO.Client.Signaling;

import java.io.IOException;
import java.io.UnsupportedEncodingException;
import java.net.MalformedURLException;
import java.util.Date;
import java.util.concurrent.TimeUnit;

import plugins.CENO.Client.CENOClient;
import freenet.client.FetchException;
import freenet.client.FetchException.FetchExceptionMode;
import freenet.client.FetchResult;
import freenet.client.InsertException;
import freenet.client.async.PersistenceDisabledException;
import freenet.keys.FreenetURI;
import freenet.node.FSParseException;
import freenet.support.Logger;
import freenet.support.SimpleFieldSet;

public class ChannelMaker implements Runnable {
	private FreenetURI signalSSK;
	private FreenetURI signalSSKpub;
	private boolean channelEstablished = false;
	private ChannelStatus channelStatus = ChannelStatus.starting;
	private Date lastSynced = new Date(0);

	public ChannelMaker() {
		this(null);
	}

	public ChannelMaker(String signalSSKString) {
		try {
			if (signalSSKString != null) {
				this.signalSSK = new FreenetURI(signalSSKString);
				this.signalSSKpub = this.signalSSK.deriveRequestURIFromInsertURI();
				channelStatus = ChannelStatus.waitingForSyn;
			} else {
				FreenetURI newKeyPair[] = CENOClient.nodeInterface.generateKeyPair();
				this.signalSSK = newKeyPair[0];
				this.signalSSKpub = newKeyPair[1];
			}
		} catch (MalformedURLException e) {
			this.channelStatus = ChannelStatus.fatal;
		}
	}

	@Override
	public void run() {
		if(!checkChannelEstablished()) {
			if(ChannelStatus.isFatalStatus(channelStatus)) {
				return;
			}
			establishChannel();
		}
	}

	public String getSignalSSK() {
		return signalSSK.toASCIIString();
	}

	public boolean isFatal() {
		return ChannelStatus.isFatalStatus(channelStatus);
	}

	public boolean canSend() {
		return ChannelStatus.canSend(channelStatus);
	}

	private boolean checkChannelEstablished() {
		if(!canSend()) {
			return false;
		}

		FreenetURI synURI = new FreenetURI("USK", "syn", signalSSKpub.getRoutingKey(), signalSSKpub.getCryptoKey(), signalSSKpub.getExtra());
		FetchResult fetchResult = null;
		while(!channelEstablished) {
			try {
				fetchResult = CENOClient.nodeInterface.fetchURI(synURI);
			} catch (FetchException e) {
				if(e.getMode() == FetchExceptionMode.PERMANENT_REDIRECT) {
					synURI = e.newURI;
				} else if(e.isDNF() || e.isFatal()) {
					break;
				}
			}
			if(fetchResult != null) {
				try {
					Long synDate = Long.parseLong(new String(fetchResult.asByteArray()));
					if(System.currentTimeMillis() - synDate > TimeUnit.DAYS.toMillis(25)) {
						establishChannel();
						break;
					}
				} catch (IOException e) {
					channelStatus = ChannelStatus.failedToParseSyn;
					break;
				}
				channelStatus = ChannelStatus.syn;
				channelEstablished = true;
			}
		}
		return channelEstablished;
	}

	private void establishChannel() {
		FreenetURI bridgeKey;
		try {
			bridgeKey = new FreenetURI(CENOClient.BRIDGE_KEY);
		} catch (MalformedURLException e1) {
			channelStatus = ChannelStatus.fatal;
			return;
		}
		FreenetURI bridgeSignalerURI = new FreenetURI("USK", "CENO-signaler", bridgeKey.getRoutingKey(), bridgeKey.getCryptoKey(), bridgeKey.getExtra());
		FetchResult bridgeSignalFreesite = null;
		while(bridgeSignalFreesite == null) {
			try {
				bridgeSignalFreesite = CENOClient.nodeInterface.fetchURI(bridgeSignalerURI);
			} catch (FetchException e) {
				if (e.mode == FetchException.FetchExceptionMode.PERMANENT_REDIRECT) {
					bridgeSignalerURI = e.newURI;
					continue;
				}
				if (e.isFatal()) {
					channelStatus = ChannelStatus.failedToGetSignalSSK;
					return;
				}
				Logger.warning(this, "Exception while retrieving the bridge's signal page: " + e.getMessage());
			}
		}
		SimpleFieldSet sfs;
		String question = null;
		try {
			sfs = new SimpleFieldSet(new String(bridgeSignalFreesite.asByteArray()), false, true, true);
			question = sfs.getString("question");
		} catch (IOException e) {
			Logger.error(this, "IOException while reading the CENO-signaler page");
		} catch (FSParseException e) {
			Logger.error(this, "Exception while parsing the SFS of the CENO-signaler");
		}
		if (question == null) {
			// CENO Client won't be able to signal the bridge
			//TODO Terminate plugin
			channelStatus = ChannelStatus.failedToSolvePuzzle;
			return;
		}
		SimpleFieldSet replySfs = new SimpleFieldSet(true);
		replySfs.put("id", (int) (Math.random() * (Integer.MAX_VALUE * 0.8)));
		replySfs.putOverwrite("insertURI", signalSSK.toASCIIString());
		//TODO Encrypt singalSSK
		FreenetURI insertedKSK = null;
		try {
			insertedKSK = CENOClient.nodeInterface.insertSingleChunk(new FreenetURI("KSK@" + question), replySfs.toOrderedString(),
					CENOClient.nodeInterface.getVoidPutCallback("Inserted private SSK key in the KSK@solution to the puzzle published by the bridge", "Failed to publish KSK@solution"));
		} catch (UnsupportedEncodingException e) {
			channelStatus = ChannelStatus.failedToPublishKSK;
		} catch (PersistenceDisabledException e) {
			channelStatus = ChannelStatus.failedToPublishKSK;
		} catch (MalformedURLException e) {
			channelStatus = ChannelStatus.failedToPublishKSK;
		} catch (InsertException e) {
			channelStatus = ChannelStatus.failedToPublishKSK;
		}
		if(insertedKSK == null) {
			return;
		}
		checkChannelEstablished();
	}

}
