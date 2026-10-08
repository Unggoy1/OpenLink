export namespace main {
	
	export class VoteOption {
	    id: string;
	    name: string;
	    votes: number;
	    thumbs: string[];
	
	    static createFrom(source: any = {}) {
	        return new VoteOption(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.votes = source["votes"];
	        this.thumbs = source["thumbs"];
	    }
	}
	export class BallotView {
	    round: number;
	    options: VoteOption[];
	    mine: number;
	    closed: boolean;
	    winner: number;
	    remaining: number;
	    startsIn: number;
	
	    static createFrom(source: any = {}) {
	        return new BallotView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.round = source["round"];
	        this.options = this.convertValues(source["options"], VoteOption);
	        this.mine = source["mine"];
	        this.closed = source["closed"];
	        this.winner = source["winner"];
	        this.remaining = source["remaining"];
	        this.startsIn = source["startsIn"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MatchView {
	    phase: string;
	    name: string;
	    thumbs: string[];
	
	    static createFrom(source: any = {}) {
	        return new MatchView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.phase = source["phase"];
	        this.name = source["name"];
	        this.thumbs = source["thumbs"];
	    }
	}
	export class ServerView {
	    id: string;
	    key: string;
	    name: string;
	    description: string;
	    region: string;
	    status: string;
	    joinable: boolean;
	    build: string;
	    buildMatch: boolean;
	    version: string;
	    needsUpdate: boolean;
	    players: number;
	    reachability: string;
	    pingMs: number;
	    favorite: boolean;
	    match?: MatchView;
	
	    static createFrom(source: any = {}) {
	        return new ServerView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.key = source["key"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.region = source["region"];
	        this.status = source["status"];
	        this.joinable = source["joinable"];
	        this.build = source["build"];
	        this.buildMatch = source["buildMatch"];
	        this.version = source["version"];
	        this.needsUpdate = source["needsUpdate"];
	        this.players = source["players"];
	        this.reachability = source["reachability"];
	        this.pingMs = source["pingMs"];
	        this.favorite = source["favorite"];
	        this.match = this.convertValues(source["match"], MatchView);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Settings {
	    directory: string;
	    mode: string;
	    installDir: string;
	    favorites: string[];
	    muteVoteSound: boolean;
	    overlayMode: string;
	    overlayCorner: string;
	    overlayOpenKey: string;
	    overlayVoteKeys: string[];
	    overlayNoController: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.directory = source["directory"];
	        this.mode = source["mode"];
	        this.installDir = source["installDir"];
	        this.favorites = source["favorites"];
	        this.muteVoteSound = source["muteVoteSound"];
	        this.overlayMode = source["overlayMode"];
	        this.overlayCorner = source["overlayCorner"];
	        this.overlayOpenKey = source["overlayOpenKey"];
	        this.overlayVoteKeys = source["overlayVoteKeys"];
	        this.overlayNoController = source["overlayNoController"];
	    }
	}
	export class StatusView {
	    serverId: string;
	    serverName: string;
	    gameName: string;
	    autoMode: boolean;
	    mode: string;
	    beaconAge: number;
	    connected: boolean;
	    upKB: number;
	    downKB: number;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new StatusView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serverId = source["serverId"];
	        this.serverName = source["serverName"];
	        this.gameName = source["gameName"];
	        this.autoMode = source["autoMode"];
	        this.mode = source["mode"];
	        this.beaconAge = source["beaconAge"];
	        this.connected = source["connected"];
	        this.upKB = source["upKB"];
	        this.downKB = source["downKB"];
	        this.error = source["error"];
	    }
	}
	export class UpdateInfo {
	    current: string;
	    latest: string;
	    url: string;
	    available: boolean;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.current = source["current"];
	        this.latest = source["latest"];
	        this.url = source["url"];
	        this.available = source["available"];
	    }
	}

}

