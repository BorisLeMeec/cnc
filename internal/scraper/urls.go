package scraper

// CommissionURL is a known CNC Talent commission page URL.
// Some pages are "consolidated" and cover multiple sessions from the same year.
type CommissionURL struct {
	URL          string
	Consolidated bool // if true, the page contains multiple sessions
}

// KnownURLs is the complete list of CNC Talent commission pages,
// ordered newest → oldest. Last verified: March 2026.
var KnownURLs = []CommissionURL{
	// 2025
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-12-novembre-2025_2503391"},
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-17-septembre-2025_2486647"},
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-3-juillet-2025_2431454"},
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-23-mai-2025_2413837"},

	// 2024
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-20-novembre-2024_2322707"},
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-18-septembre-2024_2282280"},
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-27-juin-2024_2230544"},
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-30-avril-2024_2201807"},
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-15-mars-2024_2190044"},

	// 2023
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-22-novembre-2023_2130283", Consolidated: true},
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-18-septembre-2023_2053517"},
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-17-avril-2023_1967500"},

	// 2022
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-14-octobre-2022_1829503", Consolidated: true},
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-15-avril-2022_1697860"},

	// 2021
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-7-decembre-2021_1628468", Consolidated: true},
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-12-octobre-2021_1578488"},
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-17-juin-2021_1517497"},
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-2-avril-2021_1461054"},
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-11-decembre-2020_1410353"},

	// 2020
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-15-octobre-2020_1395148", Consolidated: true},
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-18-juin-2020_1367472"},
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-18-juin-2020_1287195"},
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-15-octobre-2019_1170072"},
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-15-octobre-2019_1170060"},

	// 2019
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-18-juin-2019_1097133", Consolidated: true},
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-19-avril-2019_1045254"},
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-19-avril-2019_1036166"},
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-21-fevrier-2019_969312"},

	// 2018
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-13-decembre-2018_939596"},
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-19-octobre-2018_900051"},
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-24-avril-2018_834996"},
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-19-mars-2018_759076"},
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-des-commissions_570218"},

	// 2017
	{URL: "https://www.cnc.fr/professionnels/aides-et-financements/resultats-commissions/fonds-daide-aux-createurs-video-sur-internet-cnc-talent--resultats-de-la-commission-du-4-decembre-2017_759084"},
}
