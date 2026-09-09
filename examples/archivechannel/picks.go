//go:build wasip1

// The curated core: the titles this channel vouches for (D178 ruling 1).
//
// Why a table and not a query: the archive has no content classification
// to filter on, its `publicdate desc` order is yesterday's bootleg rips,
// and its most-watched-this-week is pornography. Section 3 of
// docs/ARCHIVE-CHANNEL.md has the measurements. So curation here is an
// ALLOWLIST somebody read, and the queries in query.go sit behind it as
// the deep catalog.
//
// Why each row carries a TMDB id: `meta` on a synced item is written
// straight to items.meta_provider / items.meta_id, and a channel library's
// provider is `tmdb` by default, so the host enriches these titles BY ID.
// Real posters, real backdrops, cast, genres and certifications, with no
// search anywhere. A row with no tmdb id is stamped `none` by the host and
// correctly skipped, which is what every uncurated catalog title gets.
//
// Why a curated row sends no poster: the archive's own thumbnail is
// 180x124, and enrichment overwrites it a moment later anyway. Sending
// nothing keeps a stretched thumbnail from being what somebody sees in the
// window before TMDB lands.
package main

// pick is one vouched-for title.
//
// ia is the archive identifier, which is also the externalId, which is
// also the identity that makes progress and watched state survive a
// re-sync. tmdb is the TMDB movie id as a string. lib is the library key
// it belongs to. hero > 0 puts it in the banner, lowest first.
type pick struct {
	ia    string
	tmdb  string
	title string
	year  int
	lib   string
	hero  int
}

// picksFor is the curated rows for one library, in the order they should
// be emitted: a sync sends these before the catalog tail, so the good
// titles exist before anything else does.
func picksFor(lib string) []pick {
	out := []pick{}
	for _, p := range picks {
		if p.lib == lib {
			out = append(out, p)
		}
	}
	return out
}

// pickIDs is every curated identifier for one library, for the lookup that
// turns them into local item ids at render time.
func pickIDs(lib string) []string {
	out := []string{}
	for _, p := range picksFor(lib) {
		out = append(out, p.ia)
	}
	return out
}

// heroPicks is the banner, lowest `hero` first.
func heroPicks() []pick {
	out := []pick{}
	for _, p := range picks {
		if p.hero > 0 {
			out = append(out, p)
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].hero < out[j-1].hero; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// curated reports whether an identifier is in the table, so a catalog pass
// can skip a title it already emitted from here.
func curated(ia string) bool {
	for _, p := range picks {
		if p.ia == ia {
			return true
		}
	}
	return false
}

// picks was built collection by collection and checked identifier by
// identifier on 2026-09-08. Two gates, and a row that missed either one was
// dropped rather than guessed at.
//
// PLAYABLE. Every ia below came back from the search index filtered to
// format:"h.264", asked in batches, and then again from metadata/<id> with
// a real derivative in the file list. Both passes are needed, because a
// collection listing proves nothing: cliffhangers holds 59 movie serials
// and 1 playable copy, and classic_cartoons holds 81 and none.
//
// Every row here has a derivative filed as `h.264` EXACTLY, which is a
// stricter bar than the plugin itself applies. The archive spells the same
// derivative `h.264` on one item and `h.264 IA` on the next, and the search
// index treats the format as a token and returns both, so main.go's
// playableFiles matches on the prefix. Holding this table to the exact
// spelling costs a handful of titles (The Bat Whispers has no such copy and
// is not here) and buys certainty that these particular rows, the ones on
// the front page, are not resting on a match anybody has to reason about.
//
// IDENTIFIED. Every tmdb id was matched by title AND year against the TMDB
// search API and, where a title has remakes, confirmed against the movie
// record itself. Public-domain catalogs are full of same-titled films: A
// Star Is Born is four movies, The Cabinet of Dr. Caligari is three, and a
// wrong id here puts the wrong poster, the wrong cast and the wrong
// certification on somebody's film. Anything that could not be matched
// with confidence is not in this table. That is why Newsreels is short:
// most of Prelinger is ephemeral film with no TMDB entry at all, and an
// invented id would be worse than a thin shelf.
//
// The archive carries no content rating and no adult flag, so the third
// gate is a person reading the list. Horror and noir belong here; the
// archive's own most-watched sorts do not.
var picks = []pick{
	// Feature Films: the hero five first, then the rest.
	{ia: "Night.Of.The.Living.Dead_1080p", tmdb: "10331", title: "Night of the Living Dead", year: 1968, lib: "films", hero: 1},
	{ia: "his_girl_friday", tmdb: "3085", title: "His Girl Friday", year: 1940, lib: "films", hero: 2},
	{ia: "MyManGodfrey1936", tmdb: "13562", title: "My Man Godfrey", year: 1936, lib: "films", hero: 3},
	{ia: "meet_john_doe", tmdb: "32574", title: "Meet John Doe", year: 1941, lib: "films", hero: 4},
	{ia: "CarnivalofSouls", tmdb: "16093", title: "Carnival of Souls", year: 1962, lib: "films", hero: 5},
	{ia: "AStarIsBorn", tmdb: "22692", title: "A Star Is Born", year: 1937, lib: "films"},
	{ia: "little_lord_fauntleroy", tmdb: "23114", title: "Little Lord Fauntleroy", year: 1936, lib: "films"},
	{ia: "GreatExpectations1946", tmdb: "14320", title: "Great Expectations", year: 1946, lib: "films"},
	{ia: "Winterset", tmdb: "87445", title: "Winterset", year: 1936, lib: "films"},
	{ia: "penny_serenade", tmdb: "43795", title: "Penny Serenade", year: 1941, lib: "films"},
	{ia: "CottageToLet", tmdb: "71984", title: "Cottage to Let", year: 1941, lib: "films"},
	{ia: "Cyrano_DeBergerac", tmdb: "43386", title: "Cyrano de Bergerac", year: 1950, lib: "films"},
	{ia: "TheMostDangerousGame", tmdb: "1994", title: "The Most Dangerous Game", year: 1932, lib: "films"},
	{ia: "angel_on_my_shoulder", tmdb: "22688", title: "Angel on My Shoulder", year: 1946, lib: "films"},
	{ia: "secret_weapon", tmdb: "18706", title: "Sherlock Holmes and the Secret Weapon", year: 1943, lib: "films"},
	{ia: "dressed_to_kill", tmdb: "20417", title: "Dressed to Kill", year: 1946, lib: "films"},
	{ia: "They_Made_Me_A_Criminal_1939", tmdb: "26378", title: "They Made Me a Criminal", year: 1939, lib: "films"},
	{ia: "dreams-that-money-can-buy", tmdb: "32327", title: "Dreams That Money Can Buy", year: 1947, lib: "films"},
	{ia: "dishonored_lady", tmdb: "33079", title: "Dishonored Lady", year: 1947, lib: "films"},
	{ia: "Q-Planes", tmdb: "43833", title: "Q Planes", year: 1939, lib: "films"},
	{ia: "TheScarletPimpernel", tmdb: "22614", title: "The Scarlet Pimpernel", year: 1934, lib: "films"},
	{ia: "little_princess", tmdb: "26531", title: "The Little Princess", year: 1939, lib: "films"},
	{ia: "angel_and_the_badman", tmdb: "22356", title: "Angel and the Badman", year: 1947, lib: "films"},
	{ia: "Scrooge_1935", tmdb: "41118", title: "Scrooge", year: 1935, lib: "films"},
	{ia: "The_Sign_OF_Four", tmdb: "27341", title: "The Sign of Four", year: 1932, lib: "films"},
	{ia: "royal_wedding", tmdb: "18646", title: "Royal Wedding", year: 1951, lib: "films"},
	{ia: "humanbondage", tmdb: "43905", title: "Of Human Bondage", year: 1934, lib: "films"},
	{ia: "TheFrontPage1931AdolpheMenjouPatOBrienLewismiles", tmdb: "42814", title: "The Front Page", year: 1931, lib: "films"},
	{ia: "the_inspector_general", tmdb: "40206", title: "The Inspector General", year: 1949, lib: "films"},
	{ia: "Strange_Woman_movie", tmdb: "32945", title: "The Strange Woman", year: 1946, lib: "films"},
	{ia: "JungleBook", tmdb: "23033", title: "Jungle Book", year: 1942, lib: "films"},
	{ia: "BeatTheDevil1953", tmdb: "22733", title: "Beat the Devil", year: 1953, lib: "films"},
	{ia: "Mclintock.avi", tmdb: "15263", title: "McLintock!", year: 1963, lib: "films"},
	{ia: "cco2_TheLastTimeISawParis", tmdb: "57575", title: "The Last Time I Saw Paris", year: 1954, lib: "films"},
	{ia: "Fathers_Little_Dividend.avi", tmdb: "22968", title: "Father's Little Dividend", year: 1951, lib: "films"},
	{ia: "amazing_adventure", tmdb: "35810", title: "The Amazing Adventure", year: 1936, lib: "films"},
	{ia: "Great_Guy.avi", tmdb: "46351", title: "Great Guy", year: 1936, lib: "films"},
	{ia: "earthworm_tractors", tmdb: "38070", title: "Earthworm Tractors", year: 1936, lib: "films"},
	{ia: "Pygmalion", tmdb: "25016", title: "Pygmalion", year: 1938, lib: "films"},
	{ia: "TheRedHouse", tmdb: "30162", title: "The Red House", year: 1947, lib: "films"},

	// Film Noir.
	{ia: "Detour_movie", tmdb: "20367", title: "Detour", year: 1945, lib: "noir"},
	{ia: "kansascityconfidencial", tmdb: "21296", title: "Kansas City Confidential", year: 1952, lib: "noir"},
	{ia: "impact", tmdb: "25503", title: "Impact", year: 1949, lib: "noir"},
	{ia: "ScarletStreet", tmdb: "17058", title: "Scarlet Street", year: 1945, lib: "noir"},
	{ia: "suddenly", tmdb: "18398", title: "Suddenly", year: 1954, lib: "noir"},
	{ia: "He_Walked_By_Night.avi", tmdb: "31556", title: "He Walked by Night", year: 1948, lib: "noir"},
	{ia: "Hitch_Hiker", tmdb: "41462", title: "The Hitch-Hiker", year: 1953, lib: "noir"},
	{ia: "Martha_Ivers", tmdb: "27033", title: "The Strange Love of Martha Ivers", year: 1946, lib: "noir"},
	{ia: "Man_Who_Cheated_Himself", tmdb: "38751", title: "The Man Who Cheated Himself", year: 1950, lib: "noir"},
	{ia: "The_Scar_1948", tmdb: "25672", title: "Hollow Triumph", year: 1948, lib: "noir"},
	{ia: "The_Big_Combo_1955", tmdb: "22342", title: "The Big Combo", year: 1955, lib: "noir"},
	{ia: "thoseguysontheradio_gmail_Doa", tmdb: "18995", title: "D.O.A.", year: 1949, lib: "noir"},
	{ia: "TheStranger_0", tmdb: "20246", title: "The Stranger", year: 1946, lib: "noir"},
	{ia: "BlondeIce1948", tmdb: "41487", title: "Blonde Ice", year: 1948, lib: "noir"},
	{ia: "Please_Murder_Me_movie", tmdb: "46421", title: "Please Murder Me", year: 1956, lib: "noir"},
	{ia: "cause_for_alarm_1951", tmdb: "29835", title: "Cause for Alarm!", year: 1951, lib: "noir"},
	{ia: "Great_Flamarion_1945", tmdb: "35543", title: "The Great Flamarion", year: 1945, lib: "noir"},

	// Science Fiction and Horror.
	{ia: "ClaCinOnl_ThingsToCome", tmdb: "3596", title: "Things to Come", year: 1936, lib: "scifi"},
	{ia: "House_On_Haunted_Hill.avi", tmdb: "15856", title: "House on Haunted Hill", year: 1959, lib: "scifi"},
	{ia: "Horror_Hotel", tmdb: "39890", title: "The City of the Dead", year: 1960, lib: "scifi"},
	{ia: "Phantom_Planet", tmdb: "27717", title: "The Phantom Planet", year: 1961, lib: "scifi"},
	{ia: "the_brain_that_wouldnt_die", tmdb: "33468", title: "The Brain That Wouldn't Die", year: 1962, lib: "scifi"},
	{ia: "indestructible_man", tmdb: "48385", title: "Indestructible Man", year: 1956, lib: "scifi"},
	{ia: "TheMonolithMonsters1957", tmdb: "52196", title: "The Monolith Monsters", year: 1957, lib: "scifi"},
	{ia: "cco_attackofthegiantleeches", tmdb: "22718", title: "Attack of the Giant Leeches", year: 1959, lib: "scifi"},
	{ia: "TheMagicSword", tmdb: "26643", title: "The Magic Sword", year: 1962, lib: "scifi"},
	{ia: "Bowery_at_Midnight", tmdb: "38346", title: "Bowery at Midnight", year: 1942, lib: "scifi"},
	{ia: "Bluebeard", tmdb: "42186", title: "Bluebeard", year: 1944, lib: "scifi"},
	{ia: "The_Little_Shop_of_Horrors_60", tmdb: "24452", title: "The Little Shop of Horrors", year: 1960, lib: "scifi"},
	{ia: "SantaClausConquerstheMartians1964", tmdb: "32307", title: "Santa Claus Conquers the Martians", year: 1964, lib: "scifi"},
	{ia: "Horror_Express", tmdb: "32613", title: "Horror Express", year: 1972, lib: "scifi"},
	{ia: "lastmanonearth-1964", tmdb: "21159", title: "The Last Man on Earth", year: 1964, lib: "scifi"},
	{ia: "FromMoscowToCassiopeiamoskva-kassiopeya", tmdb: "20947", title: "Moscow-Cassiopeia", year: 1974, lib: "scifi"},

	// Comedy, features and the shorts worth a shelf slot.
	{ia: "Oh_Mr.Porter_1937", tmdb: "61483", title: "Oh, Mr. Porter!", year: 1937, lib: "comedy"},
	{ia: "NothingSacred", tmdb: "37650", title: "Nothing Sacred", year: 1937, lib: "comedy"},
	{ia: "my_favorite_brunette", tmdb: "18649", title: "My Favorite Brunette", year: 1947, lib: "comedy"},
	{ia: "AfricaScreams", tmdb: "20278", title: "Africa Screams", year: 1949, lib: "comedy"},
	{ia: "the-titfield-thunderbolt_202105", tmdb: "24381", title: "The Titfield Thunderbolt", year: 1953, lib: "comedy"},
	{ia: "StormInATeacup1937", tmdb: "57662", title: "Storm in a Teacup", year: 1937, lib: "comedy"},
	{ia: "Milky_Way_movie", tmdb: "41349", title: "The Milky Way", year: 1936, lib: "comedy"},
	{ia: "disorder_in_the_court", tmdb: "22950", title: "Disorder in the Court", year: 1936, lib: "comedy"},
	{ia: "Golf_Specialist_1930", tmdb: "37358", title: "The Golf Specialist", year: 1930, lib: "comedy"},
	{ia: "fatal_glass_of_beer", tmdb: "52280", title: "The Fatal Glass of Beer", year: 1933, lib: "comedy"},
	{ia: "his_double_life", tmdb: "164418", title: "His Double Life", year: 1933, lib: "comedy"},
	{ia: "Lonely_Wives_1931", tmdb: "156327", title: "Lonely Wives", year: 1931, lib: "comedy"},
	{ia: "Palooka", tmdb: "23284", title: "Palooka", year: 1934, lib: "comedy"},
	{ia: "ShrimpsForADay1934", tmdb: "174497", title: "Shrimps for a Day", year: 1934, lib: "comedy"},
	{ia: "abbott-and-costello-meet-frankenstein", tmdb: "3073", title: "Abbott and Costello Meet Frankenstein", year: 1948, lib: "comedy"},

	// The Silent Era.
	{ia: "Nosferatu1922", tmdb: "653", title: "Nosferatu", year: 1922, lib: "silent"},
	{ia: "silent-the-cabinet-of-dr-caligari", tmdb: "234", title: "The Cabinet of Dr. Caligari", year: 1920, lib: "silent"},
	{ia: "Metropolis1927EnglishVersion", tmdb: "19", title: "Metropolis", year: 1927, lib: "silent"},
	{ia: "silent-battleship-potemkin", tmdb: "643", title: "Battleship Potemkin", year: 1925, lib: "silent"},
	{ia: "The_General_Buster_Keaton", tmdb: "961", title: "The General", year: 1926, lib: "silent"},
	{ia: "silent-sherlock-jr-", tmdb: "992", title: "Sherlock Jr.", year: 1924, lib: "silent"},
	{ia: "silent-safety-last", tmdb: "22596", title: "Safety Last!", year: 1923, lib: "silent"},
	{ia: "dom-8270-1-steamboatbilljr", tmdb: "25768", title: "Steamboat Bill, Jr.", year: 1928, lib: "silent"},
	{ia: "our-hospitality-1923", tmdb: "701", title: "Our Hospitality", year: 1923, lib: "silent"},
	{ia: "silent-nanook-of-the-north", tmdb: "669", title: "Nanook of the North", year: 1922, lib: "silent"},
	{ia: "silent-the-phantom-of-the-opera", tmdb: "964", title: "The Phantom of the Opera", year: 1925, lib: "silent"},
	{ia: "markofzorro-1920", tmdb: "42657", title: "The Mark of Zorro", year: 1920, lib: "silent"},
	{ia: "lost_world", tmdb: "2981", title: "The Lost World", year: 1925, lib: "silent"},
	{ia: "silent-the-thief-of-bagdad", tmdb: "28963", title: "The Thief of Bagdad", year: 1924, lib: "silent"},
	{ia: "silent-a-dogs-life", tmdb: "36208", title: "A Dog's Life", year: 1918, lib: "silent"},

	// Animation, from Fleischer and Iwerks to the Blender open movies.
	{ia: "steamboat-willie-mickey", tmdb: "53565", title: "Steamboat Willie", year: 1928, lib: "cartoons"},
	{ia: "gullivers_travels1939", tmdb: "42518", title: "Gulliver's Travels", year: 1939, lib: "cartoons"},
	{ia: "fantastic.-planet.", tmdb: "16306", title: "Fantastic Planet", year: 1973, lib: "cartoons"},
	{ia: "Sita_Sings_the_Blues", tmdb: "20529", title: "Sita Sings the Blues", year: 2008, lib: "cartoons"},
	{ia: "ElephantsDream", tmdb: "9761", title: "Elephants Dream", year: 2006, lib: "cartoons"},
	{ia: "Sintel_201809", tmdb: "45745", title: "Sintel", year: 2010, lib: "cartoons"},
	{ia: "GeraldMcboingBoing", tmdb: "46990", title: "Gerald McBoing-Boing", year: 1950, lib: "cartoons"},
	{ia: "SnowWhiteWithBettyBoop1933", tmdb: "127409", title: "Snow-White", year: 1933, lib: "cartoons"},
	{ia: "BalloonLandrestored", tmdb: "163145", title: "Balloon Land", year: 1935, lib: "cartoons"},
	{ia: "TheMadDoctor1933MickeyMouseSoundCartoon", tmdb: "101806", title: "The Mad Doctor", year: 1933, lib: "cartoons"},
	{ia: "ChristmasComesButOnceAYear-1936", tmdb: "46240", title: "Christmas Comes But Once a Year", year: 1936, lib: "cartoons"},
	{ia: "Superman2.TheMechanicalMonsters", tmdb: "71129", title: "The Mechanical Monsters", year: 1941, lib: "cartoons"},

	// Newsreels and Ephemera: the ones a metadata service has heard of.
	{ia: "0771_Duck_and_Cover_12_33_20_12", tmdb: "52230", title: "Duck and Cover", year: 1951, lib: "newsreel"},
	{ia: "ThePlowThatBrokeThePlains_201503", tmdb: "145053", title: "The Plow That Broke the Plains", year: 1936, lib: "newsreel"},
	{ia: "79074TheRiver_201601", tmdb: "119767", title: "The River", year: 1938, lib: "newsreel"},
	{ia: "TheCity_201505", tmdb: "163093", title: "The City", year: 1939, lib: "newsreel"},
	{ia: "lettherebelight_201706", tmdb: "86990", title: "Let There Be Light", year: 1946, lib: "newsreel"},
	{ia: "whywefightpreludetowarreel2", tmdb: "23336", title: "Prelude to War", year: 1942, lib: "newsreel"},
	{ia: "whywefightthebattleofbritain", tmdb: "79090", title: "The Battle of Britain", year: 1943, lib: "newsreel"},
	{ia: "Designfo1956", tmdb: "1654535", title: "Design for Dreaming", year: 1956, lib: "newsreel"},
	{ia: "OneGotFa1963", tmdb: "240547", title: "One Got Fat", year: 1963, lib: "newsreel"},
	{ia: "PrivateL1947", tmdb: "122479", title: "The Private Life of a Cat", year: 1947, lib: "newsreel"},
	{ia: "isforAto1953", tmdb: "387962", title: "A Is for Atom", year: 1953, lib: "newsreel"},
	{ia: "TripDownMarketStreetrBeforeTheFire", tmdb: "144425", title: "A Trip Down Market Street", year: 1906, lib: "newsreel"},

	// Classic Television. These are SERIES, and one ia is one archive item
	// holding a whole run: 169 episodes under GreenAcresCompleteSeries, 40
	// under Stingray. Every row was read out of metadata/<id> and kept only
	// if the file list holds real episodes, which is what dropped five of
	// the most-downloaded names in the collection.
	{ia: "GreenAcresCompleteSeries", tmdb: "609", title: "Green Acres", year: 1965, lib: "tv"},
	{ia: "UFO.complete", tmdb: "2560", title: "UFO", year: 1970, lib: "tv"},
	{ia: "CaptainScarlet", tmdb: "486", title: "Captain Scarlet and the Mysterons", year: 1967, lib: "tv"},
	{ia: "FireballXL5.complete", tmdb: "4378", title: "Fireball XL5", year: 1962, lib: "tv"},
	{ia: "Stingray.Complete", tmdb: "3454", title: "Stingray", year: 1964, lib: "tv"},
	{ia: "mister-ed-s-01", tmdb: "453", title: "Mister Ed", year: 1961, lib: "tv"},
	{ia: "adam-12.-s-01", tmdb: "4660", title: "Adam-12", year: 1968, lib: "tv"},
	{ia: "KolchakTheNightStalker", tmdb: "5084", title: "Kolchak: The Night Stalker", year: 1974, lib: "tv"},
	{ia: "The_Beverly_Hillbillies", tmdb: "1930", title: "The Beverly Hillbillies", year: 1962, lib: "tv"},
	{ia: "Shogun_Miniseries", tmdb: "13862", title: "Shogun", year: 1980, lib: "tv"},
}
